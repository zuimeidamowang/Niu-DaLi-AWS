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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { AuthLayout } from '../auth-layout'

function renderLayout() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <AuthLayout>
        <section aria-label='Authentication content'>
          <h2>Account access</h2>
        </section>
      </AuthLayout>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.getState().setLoading(false)
})

describe('Authentication layout', () => {
  it('keeps authentication content in a bounded panel without fixing the document height', async () => {
    const { container } = renderLayout()
    const content = await screen.findByRole('region', {
      name: 'Authentication content',
    })

    expect(container.firstElementChild).toHaveClass('min-h-svh', 'w-full')
    expect(container.firstElementChild).not.toHaveClass(
      'h-svh',
      'overflow-hidden'
    )
    expect(content.parentElement).toHaveClass(
      'min-w-0',
      'w-full',
      'max-w-[480px]'
    )
  })

  it('keeps a long site name truncated and its home link keyboard accessible', async () => {
    const systemName = 'Business workspace with a long organization name'
    useSystemConfigStore.getState().setConfig({ systemName })
    const user = userEvent.setup()
    renderLayout()

    const brand = await screen.findByRole('heading', { name: systemName })
    const home = screen.getByRole('link', { name: new RegExp(systemName) })
    expect(brand).toHaveClass('truncate')
    expect(home).toHaveAttribute('href', '/')

    await user.tab()

    expect(home).toHaveFocus()
  })
})
