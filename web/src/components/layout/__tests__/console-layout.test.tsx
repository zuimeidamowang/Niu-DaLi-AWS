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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import {
  Sidebar,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from '@/components/ui/sidebar'

import { Header } from '../components/header'
import { PageFooterPortal } from '../components/page-footer'
import { SectionPageLayout } from '../components/section-page-layout'

describe('Console page layout', () => {
  it('keeps a long page title constrained while the content scrolls independently', () => {
    const title = 'Workspace with a long descriptive management page title'
    render(
      <SectionPageLayout>
        <SectionPageLayout.Title>{title}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <section aria-label='Page content'>Content</section>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    expect(screen.getByRole('heading', { name: title })).toHaveClass('truncate')
    expect(screen.getByRole('main')).toHaveClass(
      'min-h-0',
      'min-w-0',
      'overflow-hidden'
    )
    expect(
      screen.getByRole('region', { name: 'Page content' }).parentElement
    ).toHaveClass('min-h-0', 'overflow-auto')
  })

  it('keeps fixed content inside its allotted height and allows actions to wrap on narrow screens', () => {
    render(
      <SectionPageLayout fixedContent stackActionsOnMobile>
        <SectionPageLayout.Title>Models</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <button type='button'>Create model</button>
          <button type='button'>Import model</button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <section aria-label='Model table'>Rows</section>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    expect(
      screen.getByRole('region', { name: 'Model table' }).parentElement
    ).toHaveClass('min-h-0', 'overflow-hidden')
    expect(
      screen.getByRole('heading', { name: 'Models' }).parentElement
    ).toHaveClass('min-w-0', 'max-sm:basis-full')
    expect(
      screen.getByRole('button', { name: 'Create model' }).parentElement
    ).toHaveClass('max-w-full', 'flex-wrap')
  })

  it('renders page footer actions outside the scrolling content area', async () => {
    render(
      <SectionPageLayout>
        <SectionPageLayout.Title>Settings</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <section aria-label='Settings content'>Fields</section>
          <PageFooterPortal>
            <button type='button'>Save settings</button>
          </PageFooterPortal>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    const save = await screen.findByRole('button', { name: 'Save settings' })
    const scrollArea = screen.getByRole('region', {
      name: 'Settings content',
    }).parentElement
    expect(scrollArea).not.toContainElement(save)
    expect(save.parentElement).toHaveClass('shrink-0', 'border-t')
    expect(screen.getByRole('main')).toContainElement(save)
  })

  it('keeps the header fixed in the layout while the keyboard accessible sidebar control still toggles navigation', async () => {
    const user = userEvent.setup()
    render(
      <SidebarProvider defaultOpen>
        <Header>
          <span>Workspace</span>
        </Header>
        <Sidebar collapsible='icon'>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton render={<a href='/console' />}>
                Console overview
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </Sidebar>
      </SidebarProvider>
    )

    const header = screen.getByRole('banner')
    const trigger = within(header).getByRole('button', {
      name: 'Toggle Sidebar',
    })
    const navigationItem = screen.getByRole('link', {
      name: 'Console overview',
    })
    const sidebar = navigationItem.closest('[data-state]')
    expect(header).toHaveClass('sticky', 'top-0', 'shrink-0')
    expect(sidebar).toHaveAttribute('data-state', 'expanded')
    expect(navigationItem).toHaveClass('h-10')

    trigger.focus()
    await user.keyboard('{Enter}')

    expect(trigger).toHaveFocus()
    expect(sidebar).toHaveAttribute('data-state', 'collapsed')
    expect(sidebar).toHaveAttribute('data-collapsible', 'icon')
    expect(navigationItem).toHaveClass('group-data-[collapsible=icon]:size-8!')
  })
})
