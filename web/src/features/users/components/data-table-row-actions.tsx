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
import type { Row } from '@tanstack/react-table'
import {
  Pencil,
  Trash2,
  Power,
  PowerOff,
  ArrowUp,
  ArrowDown,
  KeyRound,
  ShieldAlert,
  Link2,
  CreditCard,
  Eye,
  Mail,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { UserSubscriptionsDialog } from '@/features/subscriptions/components/dialogs/user-subscriptions-dialog'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  manageUser,
  resetUserPasskey,
  resetUserTwoFA,
  sendUserPasswordResetEmail,
} from '../api'
import {
  USER_STATUS,
  USER_ROLE,
  ERROR_MESSAGES,
  isUserDeleted,
} from '../constants'
import { getUserActionMessage } from '../lib'
import { canManageUser, canViewUser } from '../lib/user-actions'
import type { User, ManageUserAction } from '../types'
import { UserBindingDialog } from './dialogs/user-binding-dialog'
import { useUsers } from './users-provider'

interface DataTableRowActionsProps {
  row: Row<User>
}

export function DataTableRowActions({ row }: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const user = row.original
  const viewer = useAuthStore((s) => s.auth.user)
  const hasAdminAccess = (viewer?.role ?? 0) >= ROLE.ADMIN
  const { setOpen, setCurrentRow, triggerRefresh } = useUsers()
  const [resetPasskeyOpen, setResetPasskeyOpen] = useState(false)
  const [resetTwoFAOpen, setResetTwoFAOpen] = useState(false)
  const [bindingDialogOpen, setBindingDialogOpen] = useState(false)
  const [subscriptionsDialogOpen, setSubscriptionsDialogOpen] = useState(false)
  const verification = useSecureVerification()

  const handleEdit = () => {
    setCurrentRow(user)
    setOpen('update')
  }

  const handleDelete = () => {
    setCurrentRow(user)
    setOpen('delete')
  }

  const handleManage = async (action: Exclude<ManageUserAction, 'delete'>) => {
    try {
      const result = await manageUser(user.id, action)
      if (result.success) {
        toast.success(t(getUserActionMessage(action)))
        triggerRefresh()
      } else {
        handleServerError(result, t('Failed to {{action}} user', { action }))
      }
    } catch (error) {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const handleResetPasskey = async () => {
    try {
      const result = await resetUserPasskey(user.id)
      if (result.success) {
        toast.success(t('Passkey reset successfully'))
        triggerRefresh()
      } else {
        handleServerError(result, t('Failed to reset Passkey'))
      }
    } catch (error) {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetPasskeyOpen(false)
    }
  }

  const handleResetTwoFA = async () => {
    try {
      const result = await resetUserTwoFA(user.id)
      if (result.success) {
        toast.success(t('Two-factor authentication reset'))
        triggerRefresh()
      } else {
        handleServerError(result, t('Failed to reset 2FA'))
      }
    } catch (error) {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetTwoFAOpen(false)
    }
  }

  const handleSendPasswordResetEmail = async () => {
    try {
      const proof = await verification.requestVerification({
        scope: 'user.password.reset',
        context: { target_user_id: user.id },
      })
      if (!proof) return
      const result = await sendUserPasswordResetEmail(
        user.id,
        proof.proof_token
      )
      if (result.success) {
        toast.success(t('Password reset email sent'))
        triggerRefresh()
      } else {
        handleServerError(result, t('Failed to send reset email'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to send reset email'))
    }
  }

  const isDisabled = user.status === USER_STATUS.DISABLED
  const isAdmin = user.role >= USER_ROLE.ADMIN
  const isRoot = user.role === USER_ROLE.ROOT
  const canEditUser = canManageUser(viewer, user)
  const canViewUserDetails = canViewUser(viewer, user)
  const canPromoteToUserManager =
    user.role === USER_ROLE.USER &&
    (hasAdminAccess || viewer?.role === ROLE.THIRD_LEVEL_USER)
  const canPromoteToThirdLevel = user.role === USER_ROLE.USER && hasAdminAccess

  if (isUserDeleted(user) || !canViewUserDetails) {
    return null
  }

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={handleEdit}
              aria-label={t(canEditUser ? 'Edit' : 'View details')}
            />
          }
        >
          {canEditUser ? <Pencil /> : <Eye />}
        </TooltipTrigger>
        <TooltipContent>
          {t(canEditUser ? 'Edit' : 'View details')}
        </TooltipContent>
      </Tooltip>

      {canEditUser && (
        <DataTableRowActionMenu
          ariaLabel={t('Open menu')}
          contentClassName='w-48'
        >
          {isDisabled ? (
            <DropdownMenuItem onClick={() => handleManage('enable')}>
              {t('Enable')}
              <DropdownMenuShortcut>
                <Power size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem
              onClick={() => handleManage('disable')}
              disabled={isRoot}
            >
              {t('Disable')}
              <DropdownMenuShortcut>
                <PowerOff size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {canPromoteToUserManager && (
            <DropdownMenuItem onClick={() => handleManage('promote_manager')}>
              {t('Assign User Manager')}
              <DropdownMenuShortcut>
                <ArrowUp size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {canPromoteToThirdLevel && (
            <DropdownMenuItem
              onClick={() => handleManage('promote_third_level')}
            >
              {t('Assign Third-level User')}
              <DropdownMenuShortcut>
                <ArrowUp size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {hasAdminAccess && (
            <>
              {user.role > USER_ROLE.USER && !isRoot && (
                <DropdownMenuItem onClick={() => handleManage('demote')}>
                  {t('Demote')}
                  <DropdownMenuShortcut>
                    <ArrowDown size={16} />
                  </DropdownMenuShortcut>
                </DropdownMenuItem>
              )}

              {!isAdmin && viewer?.role === ROLE.SUPER_ADMIN && (
                <DropdownMenuItem onClick={() => handleManage('promote')}>
                  {t('Promote')}
                  <DropdownMenuShortcut>
                    <ArrowUp size={16} />
                  </DropdownMenuShortcut>
                </DropdownMenuItem>
              )}

              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setBindingDialogOpen(true)
                }}
              >
                {t('Manage Bindings')}
                <DropdownMenuShortcut>
                  <Link2 size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>

              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setSubscriptionsDialogOpen(true)
                }}
              >
                {t('Manage Subscriptions')}
                <DropdownMenuShortcut>
                  <CreditCard size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>

              {user.email && (
                <DropdownMenuItem
                  onSelect={(event) => {
                    event.preventDefault()
                    void handleSendPasswordResetEmail()
                  }}
                >
                  {t('Send reset email')}
                  <DropdownMenuShortcut>
                    <Mail size={16} />
                  </DropdownMenuShortcut>
                </DropdownMenuItem>
              )}

              <DropdownMenuSeparator />

              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setResetPasskeyOpen(true)
                }}
                disabled={isRoot}
              >
                {t('Reset Passkey')}
                <DropdownMenuShortcut>
                  <KeyRound size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>

              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setResetTwoFAOpen(true)
                }}
                disabled={isRoot}
              >
                {t('Reset 2FA')}
                <DropdownMenuShortcut>
                  <ShieldAlert size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>

              <DropdownMenuSeparator />
            </>
          )}

          <DropdownMenuItem
            onClick={handleDelete}
            className='text-destructive focus:text-destructive'
            disabled={isRoot}
          >
            {t('Delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DataTableRowActionMenu>
      )}

      <ConfirmDialog
        open={resetPasskeyOpen}
        onOpenChange={setResetPasskeyOpen}
        title={t('Reset Passkey')}
        desc={t(
          'Reset Passkey for {{username}}? The user will need to register a new Passkey before using passwordless login.',
          { username: user.username }
        )}
        confirmText={t('Reset Passkey')}
        handleConfirm={handleResetPasskey}
      />

      <ConfirmDialog
        open={resetTwoFAOpen}
        onOpenChange={setResetTwoFAOpen}
        title={t('Reset Two-Factor Authentication')}
        desc={t(
          'Reset 2FA for {{username}}? The user must set up 2FA again to continue using it.',
          { username: user.username }
        )}
        confirmText={t('Reset 2FA')}
        handleConfirm={handleResetTwoFA}
      />

      {hasAdminAccess && (
        <>
          <UserBindingDialog
            open={bindingDialogOpen}
            onOpenChange={setBindingDialogOpen}
            userId={user.id}
            onUnbindSuccess={triggerRefresh}
          />

          <UserSubscriptionsDialog
            open={subscriptionsDialogOpen}
            onOpenChange={setSubscriptionsDialogOpen}
            user={{ id: user.id, username: user.username }}
            onSuccess={triggerRefresh}
          />
        </>
      )}
      <SecureVerificationDialog {...verification.dialogProps} />
    </div>
  )
}
