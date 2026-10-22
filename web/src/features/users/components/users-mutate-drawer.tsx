/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Combobox } from '@/components/ui/combobox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  EMPTY_PERMISSION_CATALOG,
  hasPermission,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  createUser,
  updateUser,
  getUser,
  getUserGroupNames,
  getPermissionCatalog,
} from '../api'
import {
  BINDING_FIELDS,
  ERROR_MESSAGES,
  SUCCESS_MESSAGES,
  USER_ROLE,
} from '../constants'
import {
  userFormSchema,
  type UserFormValues,
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
} from '../lib'
import {
  canManageUser,
  canViewUser,
  isDelegatedUserManager,
} from '../lib/user-actions'
import type { User } from '../types'
import { UserQuotaDialog } from './user-quota-dialog'
import { useUsers } from './users-provider'

type UsersMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: User
}

export function UsersMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: UsersMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const { triggerRefresh } = useUsers()
  const currentUser = useAuthStore((s) => s.auth.user)
  const isUserManager = currentUser?.role === ROLE.USER_MANAGER
  const isThirdLevelUser = currentUser?.role === ROLE.THIRD_LEVEL_USER
  const hasAdminAccess = (currentUser?.role ?? 0) >= ROLE.ADMIN
  const isDelegatedManager = isDelegatedUserManager(currentUser?.role)
  const [groupInherited, setGroupInherited] = useState(
    currentRow?.group_inherited ?? false
  )
  const canEditUser =
    (currentUser?.role ?? 0) >= ROLE.USER_MANAGER &&
    (!currentRow || canManageUser(currentUser, currentRow))
  const canViewCurrentUser = !currentRow || canViewUser(currentUser, currentRow)
  const isReadOnly = !!currentRow && canViewCurrentUser && !canEditUser
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [quotaDialogOpen, setQuotaDialogOpen] = useState(false)
  const verification = useSecureVerification()
  const cancelVerification = verification.cancel
  useEffect(() => {
    if (!open) cancelVerification()
  }, [open, cancelVerification])

  // Fetch groups
  const { data: groupsData } = useQuery({
    queryKey: ['admin-user-group-names'],
    queryFn: async () => requireServerSuccess(await getUserGroupNames()),
    staleTime: 5 * 60 * 1000,
    enabled: open && (currentUser?.role ?? 0) >= ROLE.ADMIN,
  })

  const userGroups = isDelegatedManager
    ? [currentUser?.group || 'default']
    : groupsData?.data || []

  // Permission catalog is owned by the backend; fetched once and reused.
  const { data: permissionCatalog = EMPTY_PERMISSION_CATALOG } = useQuery({
    queryKey: ['admin-permission-catalog'],
    queryFn: async () => requireServerSuccess(await getPermissionCatalog()),
    staleTime: 5 * 60 * 1000,
    enabled: open && currentUser?.role === ROLE.SUPER_ADMIN,
  })

  const form = useForm<UserFormValues>({
    resolver: zodResolver(userFormSchema),
    defaultValues: USER_FORM_DEFAULT_VALUES,
  })

  // Load existing data when updating
  useEffect(() => {
    let cancelled = false
    if (open && isUpdate && currentRow && canViewCurrentUser) {
      setGroupInherited(currentRow.group_inherited ?? false)
      // For update, fetch fresh data
      getUser(currentRow.id)
        .then((result) => {
          if (cancelled) return
          if (result.success && result.data) {
            setGroupInherited(result.data.group_inherited ?? false)
            form.reset(transformUserToFormDefaults(result.data))
          } else {
            handleServerError(result, t('Failed to load'))
          }
        })
        .catch(
          (error) => !cancelled && handleServerError(error, t('Failed to load'))
        )
    } else if (open && !isUpdate) {
      setGroupInherited(false)
      // For create, reset to defaults
      form.reset(USER_FORM_DEFAULT_VALUES)
    }
    return () => {
      cancelled = true
    }
  }, [open, isUpdate, currentRow, canViewCurrentUser, form, t])

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const currentQuotaRaw = form.watch('quota_dollars') || 0
  const selectedRole = form.watch('role')
  const selectedUserGroup = form.watch('group')
  const userGroupOptions = [...userGroups]
  if (
    !isDelegatedManager &&
    selectedUserGroup &&
    !userGroupOptions.includes(selectedUserGroup)
  ) {
    userGroupOptions.push(selectedUserGroup)
  }
  const canEditAdminPermissions =
    currentUser?.role === ROLE.SUPER_ADMIN && !isReadOnly
  const targetIsAdmin = (selectedRole ?? currentRow?.role ?? 0) >= ROLE.ADMIN
  const commonRoleOption = {
    value: String(USER_ROLE.USER),
    label: t('Common User'),
  }
  const managerRoleOption = {
    value: String(USER_ROLE.USER_MANAGER),
    label: t('User Manager'),
  }
  const thirdLevelRoleOption = {
    value: String(USER_ROLE.THIRD_LEVEL_USER),
    label: t('Third-level User'),
  }
  const adminRoleOption = {
    value: String(USER_ROLE.ADMIN),
    label: t('Admin'),
  }
  let createRoleOptions = [
    commonRoleOption,
    managerRoleOption,
    thirdLevelRoleOption,
  ]
  if (isThirdLevelUser) {
    createRoleOptions = [commonRoleOption, managerRoleOption]
  } else if (isUserManager) {
    createRoleOptions = [commonRoleOption]
  } else if (currentUser?.role === ROLE.SUPER_ADMIN) {
    createRoleOptions = [...createRoleOptions, adminRoleOption]
  }
  let drawerTitle = `${t(isUpdate ? 'Update' : 'Create')} ${t('User')}`
  let drawerDescription = isUpdate
    ? t('Update the user by providing necessary info.')
    : t('Add a new user by providing necessary info.')
  if (isReadOnly) {
    drawerTitle = t('View details')
    drawerDescription = t('Details')
  }

  const onSubmit = async (data: UserFormValues) => {
    if (!canEditUser) return
    if (!isUpdate || data.password) {
      if (!accountPasswordSchema.safeParse(data.password ?? '').success) {
        form.setError('password', {
          type: 'manual',
          message: t('Password must contain between 8 and 128 characters.'),
        })
        return
      }
    }

    setIsSubmitting(true)
    try {
      let proofToken: string | undefined
      if (isDelegatedManager && currentRow && data.password) {
        const proof = await verification.requestVerification({
          scope: 'user.password.reset',
          context: { target_user_id: currentRow.id },
        })
        if (!proof) return
        proofToken = proof.proof_token
      }
      const payload = transformFormDataToPayload(
        data,
        currentRow?.id,
        canEditAdminPermissions ? permissionCatalog : undefined
      )
      if (isDelegatedManager) {
        if (!isUpdate) {
          payload.role =
            isThirdLevelUser && data.role === USER_ROLE.USER_MANAGER
              ? USER_ROLE.USER_MANAGER
              : ROLE.USER
        }
        delete payload.group
      }
      const result = isUpdate
        ? await updateUser(
            payload as typeof payload & { id: number },
            proofToken
          )
        : await createUser(payload)

      if (result.success) {
        toast.success(
          isUpdate
            ? t(SUCCESS_MESSAGES.USER_UPDATED)
            : t(SUCCESS_MESSAGES.USER_CREATED)
        )
        onOpenChange(false)
        triggerRefresh()
      } else {
        handleServerError(result, t(ERROR_MESSAGES.CREATE_FAILED))
      }
    } catch (error) {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsSubmitting(false)
    }
  }

  const refreshUserData = async () => {
    if (!currentRow) return
    try {
      const result = requireServerSuccess(await getUser(currentRow.id))
      if (result.success && result.data) {
        form.reset(transformUserToFormDefaults(result.data))
      }
      triggerRefresh()
    } catch (error) {
      handleServerError(error, t('Failed to load'))
    }
  }

  return (
    <>
      <Sheet
        open={open && !verification.isActive}
        onOpenChange={(v) => {
          onOpenChange(v)
          if (!v) {
            form.reset()
          }
        }}
      >
        <SheetContent
          className={sideDrawerContentClassName('sm:max-w-[600px]')}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{drawerTitle}</SheetTitle>
            <SheetDescription>{drawerDescription}</SheetDescription>
            {isDelegatedManager && !isUpdate && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'New users belong to you, inherit your group, and start with zero quota.'
                )}
              </p>
            )}
          </SheetHeader>
          <Form {...form}>
            <form
              id='user-form'
              onSubmit={form.handleSubmit(onSubmit)}
              className={sideDrawerFormClassName()}
            >
              {/* Basic Information */}
              <SideDrawerSection>
                <h3 className='text-sm font-medium'>
                  {t('Basic Information')}
                </h3>

                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Username')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('Enter username')}
                          disabled={isUpdate}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {!isUpdate && !isUserManager && (
                  <FormField
                    control={form.control}
                    name='role'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Role')}</FormLabel>
                        <Select
                          items={createRoleOptions}
                          onValueChange={(value) =>
                            value !== null &&
                            field.onChange(Number.parseInt(value))
                          }
                          value={String(field.value)}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue placeholder={t('Select a role')} />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {createRoleOptions.map((option) => (
                                <SelectItem
                                  key={option.value}
                                  value={option.value}
                                >
                                  {option.label}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormDescription>
                          {t("Set the user's role (cannot be Root)")}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='display_name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Display Name')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('Enter display name')}
                          disabled={isReadOnly}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Leave empty to use username')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {!isReadOnly && (!isUpdate || !hasAdminAccess) && (
                  <FormField
                    control={form.control}
                    name='password'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Password')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            type='password'
                            placeholder={
                              isUpdate
                                ? t('Leave empty to keep unchanged')
                                : t('Enter password (8–128 characters)')
                            }
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}
              </SideDrawerSection>

              {/* Group & Quota Settings (Update only) */}
              {isUpdate && (
                <SideDrawerSection>
                  <h3 className='text-sm font-medium'>{t('Group & Quota')}</h3>

                  <FormField
                    control={form.control}
                    name='group'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Group')}</FormLabel>
                        <FormControl>
                          <Combobox
                            options={userGroupOptions.map((group) => ({
                              value: group,
                              label: group,
                            }))}
                            onValueChange={field.onChange}
                            value={field.value}
                            disabled={
                              isDelegatedManager || isReadOnly || groupInherited
                            }
                            className='w-full'
                            placeholder={t('Select a group')}
                          />
                        </FormControl>
                        {groupInherited && (
                          <FormDescription>
                            {t('Group is inherited from the third-level user.')}
                          </FormDescription>
                        )}
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='quota_dollars'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Remaining Quota ({{currency}})', {
                            currency: currencyLabel,
                          })}
                        </FormLabel>
                        <div className='flex gap-2'>
                          <FormControl>
                            <Input
                              value={
                                tokensOnly
                                  ? String(field.value || 0)
                                  : (field.value || 0).toFixed(6)
                              }
                              readOnly
                              className='flex-1'
                            />
                          </FormControl>
                          {canEditUser && (
                            <Button
                              type='button'
                              variant='outline'
                              onClick={() => setQuotaDialogOpen(true)}
                            >
                              <Pencil className='mr-1 h-4 w-4' />
                              {t('Adjust Quota')}
                            </Button>
                          )}
                        </div>
                        <FormDescription>
                          {formatQuota(parseQuotaFromDollars(field.value || 0))}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='remark'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Remark')}</FormLabel>
                        <FormControl>
                          <Textarea
                            {...field}
                            placeholder={t(
                              'Admin notes (only visible to admins)'
                            )}
                            rows={3}
                            disabled={isReadOnly}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SideDrawerSection>
              )}

              {canEditAdminPermissions &&
                targetIsAdmin &&
                permissionCatalog.resources.length > 0 && (
                  <SideDrawerSection>
                    <h3 className='text-sm font-medium'>
                      {t('Admin Permissions')}
                    </h3>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Default administrator permissions can be overridden for this user.'
                      )}
                    </p>
                    <FormField
                      control={form.control}
                      name='admin_permissions'
                      render={({ field }) => {
                        const selected = normalizeAdminPermissions(
                          field.value,
                          permissionCatalog
                        )
                        return (
                          <FormItem>
                            <div className='space-y-3'>
                              {permissionCatalog.resources.map((resource) => (
                                <div
                                  key={resource.resource}
                                  className='space-y-2 rounded-md border p-3'
                                >
                                  <div className='text-sm font-medium'>
                                    {t(resource.label_key)}
                                  </div>
                                  <div className='space-y-2'>
                                    {resource.actions.map((option) => (
                                      <label
                                        key={option.action}
                                        className='flex items-start gap-3'
                                      >
                                        <Checkbox
                                          checked={
                                            selected[resource.resource]?.[
                                              option.action
                                            ] === true
                                          }
                                          onCheckedChange={(checked) => {
                                            field.onChange({
                                              ...selected,
                                              [resource.resource]: {
                                                ...selected[resource.resource],
                                                [option.action]:
                                                  checked === true,
                                              },
                                            })
                                          }}
                                        />
                                        <span className='flex flex-col gap-1'>
                                          <span className='text-sm font-medium'>
                                            {t(option.label_key)}
                                          </span>
                                          <span className='text-muted-foreground text-xs'>
                                            {t(option.description_key)}
                                          </span>
                                        </span>
                                      </label>
                                    ))}
                                  </div>
                                </div>
                              ))}
                            </div>
                            <FormMessage />
                          </FormItem>
                        )
                      }}
                    />
                    {currentUser && (
                      <p className='text-muted-foreground text-xs'>
                        {hasPermission(
                          currentUser,
                          ADMIN_PERMISSION_RESOURCES.CHANNEL,
                          ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
                        )
                          ? t(
                              'Your account can edit sensitive channel settings.'
                            )
                          : t(
                              'Your account cannot edit sensitive channel settings.'
                            )}
                      </p>
                    )}
                  </SideDrawerSection>
                )}

              {/* Binding Information (Read-only) */}
              {isUpdate && !isDelegatedManager && (
                <SideDrawerSection>
                  <h3 className='text-sm font-medium'>
                    {t('Binding Information')}
                  </h3>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Third-party account bindings (read-only, managed by user in Security & Access)'
                    )}
                  </p>

                  <div className='flex flex-col gap-3'>
                    {BINDING_FIELDS.map(({ key, label }) => (
                      <div key={key}>
                        <Label className='text-muted-foreground text-xs'>
                          {t(label)}
                        </Label>
                        <Input
                          value={
                            (currentRow?.[key as keyof User] as string) || '-'
                          }
                          disabled
                          className='mt-1'
                        />
                      </div>
                    ))}
                  </div>
                </SideDrawerSection>
              )}
            </form>
          </Form>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose render={<Button variant='outline' />}>
              {t('Close')}
            </SheetClose>
            {canEditUser && (
              <Button form='user-form' type='submit' disabled={isSubmitting}>
                {isSubmitting ? t('Saving...') : t('Save changes')}
              </Button>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <SecureVerificationDialog {...verification.dialogProps} />

      {/* Adjust Quota Dialog */}
      {currentRow && canEditUser && (
        <UserQuotaDialog
          open={quotaDialogOpen}
          onOpenChange={setQuotaDialogOpen}
          userId={currentRow.id}
          currentQuota={parseQuotaFromDollars(currentQuotaRaw || 0)}
          onSuccess={refreshUserData}
        />
      )}
    </>
  )
}
