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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { ForbiddenError } from '../forbidden'
import { GeneralError } from '../general-error'
import { MaintenanceError } from '../maintenance-error'
import { NotFoundError } from '../not-found-error'
import { UnauthorisedError } from '../unauthorized-error'

function renderError(component: () => ReactNode) {
  const root = createRootRoute({ component: Outlet })
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => <h1>Workspace home</h1>,
  })
  const errorPage = createRoute({
    getParentRoute: () => root,
    path: '/error',
    component,
  })
  const router = createRouter({
    routeTree: root.addChildren([home, errorPage]),
    history: createMemoryHistory({ initialEntries: ['/error'] }),
  })
  return render(<RouterProvider router={router} />)
}

describe('Error page layout', () => {
  it.each([
    ['401', UnauthorisedError],
    ['403', ForbiddenError],
    ['404', NotFoundError],
    ['500', () => <GeneralError />],
    ['503', MaintenanceError],
  ] as const)(
    'keeps the %s page readable within a narrow viewport and allows its actions to wrap',
    async (status, component) => {
      renderError(component)
      const code = await screen.findByRole('heading', { name: status })
      const panel = code.parentElement

      expect(panel).toHaveClass('w-full', 'max-w-xl')
      expect(panel?.parentElement).toHaveClass('min-h-svh')
      expect(panel?.parentElement).not.toHaveClass('h-svh', 'overflow-hidden')
      const action = screen.getAllByRole('button')[0]
      expect(action.parentElement).toHaveClass('max-w-full', 'flex-wrap')
    }
  )

  it('keeps the home action working after the error panel is restyled', async () => {
    const user = userEvent.setup()
    renderError(NotFoundError)

    await user.click(
      await screen.findByRole('button', { name: 'Back to Home' })
    )

    expect(
      await screen.findByRole('heading', { name: 'Workspace home' })
    ).toBeVisible()
  })

  it('keeps minimal errors free of status codes and navigation actions', async () => {
    renderError(() => <GeneralError minimal />)

    expect(await screen.findByText(/Oops! Something went wrong/)).toBeVisible()
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
