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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Button } from '../button'
import { Input } from '../input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../tabs'
import { TitledCard } from '../titled-card'

describe('Shared business controls', () => {
  it('keeps default form controls aligned while preserving typing and disabled actions', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    render(
      <>
        <Input aria-label='Project name' />
        <Button onClick={onSave}>Save</Button>
        <Button disabled onClick={onSave}>
          Unavailable
        </Button>
      </>
    )

    const input = screen.getByRole('textbox', { name: 'Project name' })
    const save = screen.getByRole('button', { name: 'Save' })
    expect(input).toHaveClass('h-9')
    expect(save).toHaveClass('h-9')
    await user.type(input, 'Research')
    expect(input).toHaveValue('Research')
    await user.tab()
    expect(save).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(onSave).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole('button', { name: 'Unavailable' }))
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('keeps tab selection and keyboard navigation when the shared tab surface changes', async () => {
    const user = userEvent.setup()
    render(
      <Tabs defaultValue='overview'>
        <TabsList aria-label='Workspace views'>
          <TabsTrigger value='overview'>Overview</TabsTrigger>
          <TabsTrigger value='records'>Records</TabsTrigger>
        </TabsList>
        <TabsContent value='overview'>Overview content</TabsContent>
        <TabsContent value='records'>Record content</TabsContent>
      </Tabs>
    )

    const overview = screen.getByRole('tab', { name: 'Overview' })
    overview.focus()
    await user.keyboard('{ArrowRight}{Enter}')
    expect(screen.getByRole('tab', { name: 'Records' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Record content')
  })

  it('lets long card headings shrink while actions stack on narrow screens', () => {
    render(
      <TitledCard
        title='A descriptive settings section with a long heading'
        action={<Button>Save settings</Button>}
      >
        <Input aria-label='Configuration' />
      </TitledCard>
    )

    const title = screen.getByText(
      'A descriptive settings section with a long heading'
    )
    expect(title.parentElement).toHaveClass('min-w-0')
    const action = screen.getByRole('button', { name: 'Save settings' })
    expect(action.parentElement).toHaveClass('w-full', 'sm:w-auto')
    expect(screen.getByRole('textbox', { name: 'Configuration' })).toBeVisible()
  })
})
