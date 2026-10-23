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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { UsersProvider } from '../users-provider'
import { UsersTable } from '../users-table'

const thirdLevelUser: User = {
  id: 10,
  username: 'third-level',
  display_name: 'Third-level user',
  role: 6,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  creator_id: 0,
}

const userManager: User = {
  ...thirdLevelUser,
  id: 20,
  username: 'second-level',
  display_name: 'Second-level user',
  role: 5,
  creator_id: thirdLevelUser.id,
  group_inherited: true,
}

const commonUser: User = {
  ...thirdLevelUser,
  id: 30,
  username: 'direct-user',
  display_name: 'Direct user',
  role: 1,
  creator_id: userManager.id,
  group_inherited: true,
}

const i18n = createInstance()
await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })

function UsersPage() {
  return (
    <TooltipProvider>
      <UsersProvider>
        <UsersTable />
      </UsersProvider>
    </TooltipProvider>
  )
}

async function renderUserHierarchy(options?: {
  viewerRole?: number
  initialEntry?: string
}) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'admin',
    role: options?.viewerRole ?? 100,
  })
  const children = new Map<number, User[]>([
    [thirdLevelUser.id, [userManager]],
    [userManager.id, [commonUser]],
  ])
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/') {
      return {
        data: {
          success: true,
          data: { items: [thirdLevelUser], total: 1, page: 1, page_size: 20 },
        },
      }
    }
    const parentID = Number(url.split('?')[0].split('/').at(-1))
    return {
      data: {
        success: true,
        data: {
          items: children.get(parentID) ?? [],
          total: children.get(parentID)?.length ?? 0,
          page: 1,
          page_size: 100,
        },
      },
    }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const users = createRoute({
    getParentRoute: () => auth,
    path: 'users/',
    component: UsersPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([users])]),
    history: createMemoryHistory({
      initialEntries: [options?.initialEntry ?? '/users/'],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await screen.findByText(thirdLevelUser.username)
  return { client, get }
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it('loads only direct managed children and hides descendants when a parent is collapsed', async () => {
  const { client, get } = await renderUserHierarchy()
  const roleBadge = screen
    .getAllByText('Third-level User')
    .map((element) => element.closest('[data-slot="badge"]'))
    .find((element) => element !== null)
  const thirdLevelToggle = screen.getByRole('button', {
    name: 'Expand sub-users of third-level',
  })

  expect(roleBadge).toHaveClass('bg-chart-2/10', 'text-chart-2')
  expect(screen.getAllByText('Third-level User')[0]).toBeVisible()
  expect(thirdLevelToggle).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByText(userManager.username)).not.toBeInTheDocument()
  expect(screen.queryByText(commonUser.username)).not.toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => url.includes('/api/user/children/'))
  ).toBe(false)
  expect(get).toHaveBeenCalledWith(
    '/api/user/',
    expect.objectContaining({
      params: expect.objectContaining({ hierarchy: true }),
    })
  )

  await userEvent.click(thirdLevelToggle)
  const managerToggle = await screen.findByRole('button', {
    name: 'Expand sub-users of second-level',
  })
  expect(thirdLevelToggle).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText(userManager.username)).toBeVisible()
  await waitFor(() =>
    expect(
      get.mock.calls.some(([url]) =>
        url.startsWith(`/api/user/children/${thirdLevelUser.id}?`)
      )
    ).toBe(true)
  )

  await userEvent.click(managerToggle)
  expect(await screen.findByText(commonUser.username)).toBeVisible()

  await userEvent.click(
    screen.getByRole('button', { name: 'Collapse sub-users of third-level' })
  )
  expect(screen.queryByText(userManager.username)).not.toBeInTheDocument()
  expect(screen.queryByText(commonUser.username)).not.toBeInTheDocument()
  client.clear()
})

it('drops unsupported role filters from a third-level user URL before fetching', async () => {
  const { client, get } = await renderUserHierarchy({
    viewerRole: 6,
    initialEntry: '/users/?role=10&role=100',
  })

  await waitFor(() => {
    const rootRequest = get.mock.calls.find(([url]) => url === '/api/user/')
    expect(rootRequest).toBeDefined()
    expect(rootRequest?.[1]?.params).toMatchObject({ hierarchy: true })
    expect(rootRequest?.[1]?.params).not.toHaveProperty('role')
  })

  client.clear()
})
