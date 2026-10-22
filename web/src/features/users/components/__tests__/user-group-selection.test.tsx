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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

const targetUser: User = {
  id: 2,
  username: 'alice',
  display_name: 'Alice',
  role: 1,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'legacy-user-group',
}

function renderUserEditor() {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'admin',
    role: 10,
  })

  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user_group/') {
      return {
        data: { success: true, data: ['default', 'premium-user-group'] },
      }
    }
    if (url === `/api/user/${targetUser.id}`) {
      return { data: { success: true, data: targetUser } }
    }
    return { data: { success: true, data: [] } }
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <UsersProvider>
        <UsersMutateDrawer
          open
          onOpenChange={() => undefined}
          currentRow={targetUser}
        />
      </UsersProvider>
    </QueryClientProvider>
  )

  return { get, put }
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it('loads user groups, excludes channel groups, and preserves an unlisted historical assignment', async () => {
  const user = userEvent.setup()
  const { get, put } = renderUserEditor()

  await screen.findByDisplayValue('Alice')
  expect(get).toHaveBeenCalledWith('/api/user_group/')

  const groupSelector = screen.getByRole('combobox', { name: 'Group' })
  expect(groupSelector).toHaveValue('legacy-user-group')
  await user.click(groupSelector)

  const options = await screen.findByRole('listbox')
  expect(within(options).getByRole('option', { name: 'default' })).toBeVisible()
  expect(
    within(options).getByRole('option', { name: 'premium-user-group' })
  ).toBeVisible()
  expect(
    within(options).queryByRole('option', { name: 'legacy-user-group' })
  ).toBeVisible()
  expect(
    within(options).queryByRole('option', { name: 'channel-only-group' })
  ).not.toBeInTheDocument()
  expect(get).not.toHaveBeenCalledWith('/api/group/')

  await user.keyboard('{Escape}')
  await user.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({
        id: targetUser.id,
        group: targetUser.group,
      })
    )
  )
})
