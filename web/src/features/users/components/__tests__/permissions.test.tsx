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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { DataTableRowActions } from '../data-table-row-actions'
import { UsersDeleteDialog } from '../users-delete-dialog'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider, useUsers } from '../users-provider'

const target: User = {
  id: 2,
  username: 'managed-admin',
  display_name: 'Managed admin',
  role: 10,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
}
const label = "View other accounts' audit logs"
const description =
  'View audit records from user and admin roles. Root records are always excluded.'

function renderPermissions(
  viewerRole: number,
  allowed?: boolean,
  user: User = target,
  creating = false
) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'operator',
    role: viewerRole,
    group: 'team',
    quota: 5000000,
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/authz/catalog') {
      return {
        data: {
          success: true,
          data: {
            resources: [
              {
                resource: 'audit',
                label_key: 'Audit Logs',
                actions: [
                  {
                    action: 'read',
                    label_key: label,
                    description_key: description,
                  },
                ],
              },
            ],
            roles: [{ key: 'admin', grants: { audit: { read: false } } }],
          },
        },
      }
    }
    if (url === '/api/user_group/' || url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    return {
      data: {
        success: true,
        data: {
          ...user,
          admin_permissions:
            allowed === undefined ? {} : { audit: { read: allowed } },
        },
      },
    }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <UsersMutateDrawer
          open
          onOpenChange={() => undefined}
          currentRow={creating ? undefined : user}
        />
      </UsersProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it.each([undefined, true])(
  'root can save an audit grant or revocation from the existing editor (previous=%s)',
  async (allowed) => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    renderPermissions(100, allowed)
    await screen.findByDisplayValue('Managed admin')
    const checkbox = await screen.findByRole('checkbox', {
      name: new RegExp(label),
    })
    await waitFor(() =>
      expect(checkbox).toHaveAttribute('aria-checked', String(!!allowed))
    )
    expect(screen.getByText(description)).toBeVisible()
    await userEvent.click(checkbox)
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/user/',
        expect.objectContaining({
          id: 2,
          admin_permissions: { audit: { read: !allowed } },
        })
      )
    )
  }
)

it('admin cannot edit the audit permission even when the catalog is available', async () => {
  renderPermissions(10)
  await screen.findByDisplayValue('Managed admin')
  expect(
    screen.queryByRole('checkbox', { name: new RegExp(label) })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('textbox', { name: 'Display Name' })).toBeDisabled()
  expect(
    screen.queryByRole('button', { name: 'Save changes' })
  ).not.toBeInTheDocument()
})

it('admin can edit a third-level user group', async () => {
  const thirdLevelUser = {
    ...target,
    role: 6,
    username: 'third-level',
    display_name: 'Third-level user',
    group_inherited: false,
  }
  renderPermissions(10, undefined, thirdLevelUser)
  await screen.findByDisplayValue('Third-level user')
  expect(screen.getByRole('combobox', { name: 'Group' })).toBeEnabled()
  expect(
    screen.queryByText('Group is inherited from the third-level user.')
  ).not.toBeInTheDocument()
})

it('admin cannot change a descendant user group inherited from a third-level user', async () => {
  const inheritedUser = { ...child, group_inherited: true }
  renderPermissions(10, undefined, inheritedUser)
  await screen.findByDisplayValue('My child')
  expect(screen.getByRole('combobox', { name: 'Group' })).toBeDisabled()
  expect(
    screen.getByText('Group is inherited from the third-level user.')
  ).toBeVisible()
})

it('root can update another root profile without choosing the target password', async () => {
  const rootUser = { ...target, role: 100, username: 'other-root' }
  renderPermissions(100, undefined, rootUser)
  await screen.findByDisplayValue('Managed admin')
  expect(screen.getByRole('textbox', { name: 'Display Name' })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeVisible()
  expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
})

const child: User = {
  ...target,
  role: 1,
  username: 'child',
  display_name: 'My child',
  creator_id: 1,
  group: 'team',
}

it.each([
  { viewerRole: 5, user: child },
  { viewerRole: 6, user: { ...child, role: 5 } },
])(
  'delegated manager role $viewerRole edits a directly owned user without fetching global groups, permissions or bindings',
  async ({ viewerRole, user }) => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    renderPermissions(viewerRole, undefined, user)
    await screen.findByDisplayValue('My child')
    expect(screen.queryByText('Binding Information')).not.toBeInTheDocument()
    expect(screen.queryByText('Admin Permissions')).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Group' })).toBeDisabled()
    await userEvent.clear(screen.getByRole('textbox', { name: 'Display Name' }))
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Display Name' }),
      'Updated child'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/user/',
        expect.objectContaining({ id: 2, display_name: 'Updated child' })
      )
    )
    expect(put.mock.calls[0][1]).not.toHaveProperty('group')
    expect(put.mock.calls[0][1]).not.toHaveProperty('admin_permissions')
    expect(api.get).not.toHaveBeenCalledWith('/api/group/')
    expect(api.get).not.toHaveBeenCalledWith('/api/authz/catalog')
  }
)

it.each([5, 6])(
  'delegated manager role %s must verify their identity before resetting a child password',
  async (viewerRole) => {
    renderPermissions(viewerRole, undefined, child)
    const get = vi.mocked(api.get)
    get.mockImplementation(async (url) => {
      if (url === '/api/verify/methods') {
        return {
          data: {
            success: true,
            data: {
              scope: 'user.password.reset',
              methods: [{ method: 'password', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        }
      }
      return { data: { success: true, data: child } }
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: {
          proof_token: 'single-use-proof',
          expires_at: 9999999999,
          method: 'password',
          scope: 'user.password.reset',
        },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await screen.findByDisplayValue('My child')
    await userEvent.type(
      screen.getByLabelText('Password'),
      'new-child-password'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
    const verifyPassword = await screen.findByLabelText('Password', {
      selector: 'input',
    })
    expect(put).not.toHaveBeenCalled()
    await userEvent.type(verifyPassword, 'manager-password')
    await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/user/',
        expect.objectContaining({ id: 2, password: 'new-child-password' }),
        expect.objectContaining({
          headers: { 'X-Security-Proof': 'single-use-proof' },
          singleUseAuthorization: true,
        })
      )
    )
    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        scope: 'user.password.reset',
        context: { target_user_id: 2 },
        password: 'manager-password',
      }),
      expect.any(Object)
    )
  }
)

function RowActions(props: { user: User }) {
  const table = useReactTable({
    data: [props.user],
    columns: [{ accessorKey: 'username' }],
    getCoreRowModel: getCoreRowModel(),
  })
  return <DataTableRowActions row={table.getRowModel().rows[0]} />
}

function renderActions(user: User, viewerRole = 5) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'manager', role: viewerRole })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <UsersProvider>
          <RowActions user={user} />
        </UsersProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
}

it('administrator sends a reset email only after target-bound security verification', async () => {
  const targetUser = { ...child, email: 'child@example.com' }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/verify/methods') {
      return {
        data: {
          success: true,
          data: {
            scope: 'user.password.reset',
            methods: [{ method: 'password', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      }
    }
    return { data: { success: true, data: targetUser } }
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'reset-email-proof',
            expires_at: 9999999999,
            method: 'password',
            scope: 'user.password.reset',
          },
        },
      }
    }
    return { data: { success: true } }
  })

  renderActions(targetUser, 100)
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    screen.getByRole('menuitem', { name: 'Send reset email' })
  )
  const password = await screen.findByLabelText('Password', {
    selector: 'input',
  })
  await userEvent.type(password, 'admin-password')
  await userEvent.click(screen.getByRole('button', { name: 'Verify' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/2/reset_password',
      undefined,
      expect.objectContaining({
        headers: { 'X-Security-Proof': 'reset-email-proof' },
        singleUseAuthorization: true,
      })
    )
  )
})

it('user manager has enable, edit and delete actions for their own ordinary child only', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderActions({ ...child, status: 2 })
  expect(screen.getByRole('button', { name: 'Edit' })).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  expect(
    screen.getAllByRole('menuitem').map((item) => item.textContent)
  ).toEqual(['Enable', 'Delete'])
  await userEvent.click(screen.getByRole('menuitem', { name: 'Enable' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/manage', {
      id: 2,
      action: 'enable',
    })
  )
})

it.each([
  { ...child, creator_id: 9 },
  { ...child, role: 5 },
  { ...child, creator_id: undefined },
])(
  'user manager has no actions for a user outside their ownership and ordinary-role scope (%j)',
  (user) => {
    renderActions(user)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  }
)

it('third-level user can edit their directly owned second-level user', () => {
  renderActions({ ...child, role: 5 }, 6)
  expect(screen.getByRole('button', { name: 'Edit' })).toBeVisible()
})

it('administrator can view same-level admin details without write actions', async () => {
  renderActions({ ...child, role: 10 }, 10)
  expect(screen.getByRole('button', { name: 'View details' })).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Open menu' })
  ).not.toBeInTheDocument()
})

it('third-level user cannot manage an ordinary user created by their second-level user', () => {
  renderActions({ ...child, creator_id: child.id }, 6)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('third-level user can promote their own ordinary user to second-level', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderActions(child, 6)
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    screen.getByRole('menuitem', { name: 'Assign User Manager' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/manage', {
      id: 2,
      action: 'promote_manager',
    })
  )
})

it('administrator can assign the user manager role to an ordinary user', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderActions(child, 100)
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    screen.getByRole('menuitem', { name: 'Assign User Manager' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/manage', {
      id: 2,
      action: 'promote_manager',
    })
  )
})

it('administrator can assign an ordinary user the third-level role', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderActions(child, 10)
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    screen.getByRole('menuitem', { name: 'Assign Third-level User' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/manage', {
      id: 2,
      action: 'promote_third_level',
    })
  )
})

function DeleteChild(props: { user: User }) {
  const users = useUsers()
  return (
    <>
      <button
        type='button'
        onClick={() => {
          users.setCurrentRow(props.user)
          users.setOpen('delete')
        }}
      >
        Delete child
      </button>
      <UsersDeleteDialog />
    </>
  )
}

it.each([
  { viewerRole: 5, targetRole: 1 },
  { viewerRole: 6, targetRole: 5 },
])(
  'delegated manager role $viewerRole must reclaim child quota before deletion',
  async ({ viewerRole, targetRole }) => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'manager', role: viewerRole })
    render(
      <UsersProvider>
        <DeleteChild user={{ ...child, role: targetRole, quota: 10 }} />
      </UsersProvider>
    )
    await userEvent.click(screen.getByRole('button', { name: 'Delete child' }))
    expect(
      screen.getByText('Reclaim the remaining quota before deleting this user.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled()
  }
)

it('user manager creates only an ordinary user without a role selector or global group request', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderPermissions(5, undefined, child, true)
  expect(
    screen.queryByRole('combobox', { name: 'Role' })
  ).not.toBeInTheDocument()
  expect(
    screen.getByText(
      'New users belong to you, inherit your group, and start with zero quota.'
    )
  ).toBeVisible()
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Username' }),
    'new-child'
  )
  await userEvent.type(screen.getByLabelText('Password'), 'child-password')
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/', {
      username: 'new-child',
      display_name: 'new-child',
      password: 'child-password',
      role: 1,
    })
  )
})

it('third-level user creates a second-level user without global group requests', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderPermissions(6, undefined, child, true)
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Username' }),
    'new-manager'
  )
  await userEvent.type(screen.getByLabelText('Password'), 'manager-password')
  await userEvent.click(screen.getByRole('combobox', { name: 'Role' }))
  expect(
    await screen.findByRole('option', { name: 'Common User' })
  ).toBeVisible()
  expect(screen.getByRole('option', { name: 'User Manager' })).toBeVisible()
  expect(
    screen.queryByRole('option', { name: 'Admin' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('option', { name: 'Third-level User' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('option', { name: 'User Manager' }))
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({
        username: 'new-manager',
        role: 5,
      })
    )
  )
  expect(post.mock.calls[0][1]).not.toHaveProperty('group')
  expect(api.get).not.toHaveBeenCalledWith('/api/group/')
})

it('administrator can create a user manager through the existing role selector', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderPermissions(100, undefined, child, true)
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Username' }),
    'new-manager'
  )
  await userEvent.type(screen.getByLabelText('Password'), 'manager-password')
  await userEvent.click(screen.getByRole('combobox', { name: 'Role' }))
  await userEvent.click(
    await screen.findByRole('option', { name: 'User Manager' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({ role: 5 })
    )
  )
})

it('administrator can create a third-level user through the role selector', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderPermissions(100, undefined, child, true)
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Username' }),
    'new-third-level'
  )
  await userEvent.type(
    screen.getByLabelText('Password'),
    'third-level-password'
  )
  await userEvent.click(screen.getByRole('combobox', { name: 'Role' }))
  await userEvent.click(
    await screen.findByRole('option', { name: 'Third-level User' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({ role: 6 })
    )
  )
})

it('cancelling manager password verification does not update the child account', async () => {
  renderPermissions(5, undefined, child)
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/api/verify/methods') {
      return {
        data: {
          success: true,
          data: {
            scope: 'user.password.reset',
            methods: [{ method: 'password', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      }
    }
    return { data: { success: true, data: child } }
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  await screen.findByDisplayValue('My child')
  await userEvent.type(screen.getByLabelText('Password'), 'new-child-password')
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await screen.findByRole('dialog', { name: 'Security verification' })
  await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await screen.findByRole('button', { name: 'Save changes' })
  expect(put).not.toHaveBeenCalled()
})
