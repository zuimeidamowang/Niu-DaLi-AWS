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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, renderHook, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CommandMenu } from '@/components/command-menu'
import { checkIsActive } from '@/components/layout/lib/url-utils'
import {
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '@/features/system-settings/maintenance/config'
import { useAuthStore } from '@/stores/auth-store'

import { useSidebarConfig } from '../use-sidebar-config'
import { useSidebarData } from '../use-sidebar-data'
import { useSidebarView } from '../use-sidebar-view'

const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  cleanup()
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
  }
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

function sidebarFor(admin?: object, user?: object, canConfigure = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {
    SidebarModulesAdmin: admin ? JSON.stringify(admin) : '',
  })
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'alice',
    role: 1,
    permissions: { sidebar_settings: canConfigure },
    sidebar_modules: user ? JSON.stringify(user) : '',
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  const result = renderHook(
    () => useSidebarConfig(useSidebarData().navGroups),
    { wrapper: Wrapper }
  )
  return result
}

describe('security sidebar visibility', () => {
  it('old configurations show Security & Access immediately after Profile and keep API Keys', () => {
    const { result } = sidebarFor(
      { personal: { enabled: true, personal: true, topup: true } },
      { personal: { enabled: true, personal: true } }
    )
    expect(
      result.current
        .find((group) => group.id === 'personal')
        ?.items.map((item) => item.title)
    ).toEqual(['Wallet', 'Profile', 'Security & Access'])
    expect(
      result.current
        .flatMap((group) => group.items)
        .some((item) => item.title === 'API Keys')
    ).toBe(true)
  })
  it.each([
    [{ personal: { enabled: true, security: false } }, undefined],
    [{ personal: { enabled: false } }, { personal: { security: true } }],
    [undefined, { personal: { enabled: true, security: false } }],
    [undefined, { personal: { enabled: false } }],
  ])(
    'admin or user disablement hides Security & Access (%j, %j)',
    (admin, user) => {
      const { result } = sidebarFor(admin, user)
      expect(
        result.current
          .flatMap((group) => group.items)
          .some((item) => item.title === 'Security & Access')
      ).toBe(false)
    }
  )
  it('users without sidebar configuration permission retain the admin view', () => {
    const { result } = sidebarFor(
      undefined,
      { personal: { security: false } },
      false
    )
    expect(
      result.current
        .flatMap((group) => group.items)
        .some((item) => item.title === 'Security & Access')
    ).toBe(true)
  })
})

describe('audit log sidebar entry', () => {
  it('admin settings default Audit Logs to visible and preserve its independent toggle when saved', () => {
    const config = parseSidebarModulesAdmin(
      '{"console":{"enabled":true,"log":true}}'
    )
    expect(config.console.audit).toBe(true)
    config.console.audit = false
    const saved = parseSidebarModulesAdmin(serializeSidebarModulesAdmin(config))
    const { result } = sidebarFor(saved)
    const titles = result.current
      .flatMap((group) => group.items)
      .map((item) => item.title)
    expect(titles).not.toContain('Audit Logs')
    expect(titles).toContain('Usage Logs')
  })

  it('legacy configurations show a separate Audit Logs link immediately after Usage Logs', () => {
    const { result } = sidebarFor(
      { console: { enabled: true, log: true } },
      { console: { enabled: true, log: true } }
    )
    const items =
      result.current.find((group) => group.id === 'general')?.items ?? []
    const usageIndex = items.findIndex((item) => item.title === 'Usage Logs')
    expect(items[usageIndex + 1]).toMatchObject({
      title: 'Audit Logs',
      url: '/usage-logs/audit',
    })
    const selected = items.filter((item) =>
      checkIsActive('/usage-logs/audit', item)
    )
    expect(selected.map((item) => item.title)).toEqual(['Audit Logs'])
  })

  it.each([
    [{ console: { enabled: true, audit: false } }, undefined],
    [{ console: { enabled: false } }, { console: { audit: true } }],
    [undefined, { console: { enabled: true, audit: false } }],
    [undefined, { console: { enabled: false } }],
  ])(
    'admin and personal visibility rules can hide Audit Logs (%j, %j)',
    (admin, user) => {
      const { result } = sidebarFor(admin, user)
      expect(
        result.current
          .flatMap((group) => group.items)
          .some((item) => item.title === 'Audit Logs')
      ).toBe(false)
    }
  )

  it('hiding Usage Logs does not hide the independently configured Audit Logs entry', () => {
    const { result } = sidebarFor({ console: { enabled: true, log: false } })
    const titles = result.current
      .flatMap((group) => group.items)
      .map((item) => item.title)
    expect(titles).not.toContain('Usage Logs')
    expect(titles).toContain('Audit Logs')
  })
})

describe('chat sidebar after external canvas integration', () => {
  it('legacy chat settings retain Playground and Chat without the retired video workspace entry', () => {
    const { result } = sidebarFor({ chat: { enabled: true, playground: true } })
    expect(
      result.current.find((group) => group.id === 'chat')?.items
    ).toMatchObject([
      { title: 'Playground', url: '/playground' },
      { title: 'Chat', type: 'chat-presets' },
    ])
  })

  it.each([
    [{ chat: { enabled: false } }, undefined, []],
    [undefined, { chat: { enabled: false } }, []],
    [
      { chat: { enabled: true, videoWorkspace: false } },
      undefined,
      ['Playground', 'Chat'],
    ],
    [
      undefined,
      { chat: { enabled: true, videoWorkspace: false } },
      ['Playground', 'Chat'],
    ],
    [
      { chat: { enabled: true, videoWorkspace: true } },
      { chat: { videoWorkspace: true } },
      ['Playground', 'Chat'],
    ],
  ])(
    'chat visibility respects section settings while legacy video settings do not restore the retired entry (%j, %j)',
    (admin, user, titles) => {
      const { result } = sidebarFor(admin, user)
      expect(
        result.current
          .find((group) => group.id === 'chat')
          ?.items.map((item) => item.title) ?? []
      ).toEqual(titles)
    }
  )
})

function NavigationAccess() {
  const sidebar = useSidebarView()
  return (
    <nav>
      {sidebar.navGroups.map((group) => (
        <section key={group.id}>
          {group.items.map((item) =>
            'url' in item && item.url ? (
              <a key={item.title} href={String(item.url)}>
                {item.title}
              </a>
            ) : null
          )}
        </section>
      ))}
    </nav>
  )
}

it.each([1, 5, 6, 10, 100])(
  'role %s sees the appropriate user-management entry and admin navigation',
  async (role) => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'viewer', role })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    client.setQueryData(['status'], {})
    const root = createRootRoute({ component: NavigationAccess })
    const router = createRouter({
      routeTree: root,
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
    await router.load()
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    expect(screen.getByRole('link', { name: 'API Keys' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Profile' })).toBeVisible()
    if (role >= 5) {
      expect(screen.getByRole('link', { name: 'Users' })).toBeVisible()
    } else {
      expect(
        screen.queryByRole('link', { name: 'Users' })
      ).not.toBeInTheDocument()
    }
    if (role >= 10) {
      expect(screen.getByRole('link', { name: 'Channels' })).toBeVisible()
    } else {
      expect(
        screen.queryByRole('link', { name: 'Channels' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('link', { name: 'Subscriptions' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('link', { name: 'System Settings' })
      ).not.toBeInTheDocument()
    }
    client.clear()
  }
)

it('third-level user command palette exposes Users without global administration commands', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'manager', role: 6 })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {})
  const root = createRootRoute({
    component: () => <CommandMenu open onOpenChange={() => undefined} />,
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  expect(await screen.findByRole('option', { name: 'Users' })).toBeVisible()
  expect(
    screen.queryByRole('option', { name: 'Channels' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('option', { name: 'Subscriptions' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('option', { name: 'System Settings' })
  ).not.toBeInTheDocument()
  client.clear()
})
