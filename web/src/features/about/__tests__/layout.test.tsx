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
import { expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { About } from '..'

it('preserves project attribution and allows long repository links to wrap in the shared empty state', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: '' } })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(['about-content'], { success: true, data: '' })
  const rootRoute = createRootRoute({ component: About })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )

  expect(
    await screen.findByRole('heading', {
      name: 'No About Content Set',
      level: 2,
    })
  ).toBeVisible()
  const repository = screen.getByRole('link', {
    name: /github.com\/QuantumNous\/new-api$/,
  })
  expect(repository).toHaveAttribute(
    'href',
    'https://github.com/QuantumNous/new-api'
  )
  expect(repository.closest('[class*="overflow-wrap"]')).toHaveClass(
    '[overflow-wrap:anywhere]'
  )
  expect(screen.getByRole('link', { name: 'QuantumNous' })).toHaveAttribute(
    'href',
    'https://github.com/QuantumNous'
  )
  expect(
    screen.getByRole('link', { name: 'AGPL v3.0 License' })
  ).toHaveAttribute(
    'href',
    'https://github.com/QuantumNous/new-api/blob/main/LICENSE'
  )
  queryClient.clear()
})
