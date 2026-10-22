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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { describe, expect, it, vi } from 'vitest'
import { z } from 'zod'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { positiveIntegerSchema } from '../../utils/numeric-field'
import { GroupRatioForm } from '../group-ratio-form'
import { migrateLegacyUserGroupVisibility } from '../group-visibility-migration'

const defaults = {
  GroupRatio: '{"default":1,"vip":0.8}',
  TopupGroupRatio: '{"vip":1.2}',
  UserGroups: '{"default":"Standard account","vip":"Premium account"}',
  UserUsableGroups: '{"default":"Standard access","vip":"Premium access"}',
  GroupGroupRatio: '{}',
  AutoGroups: '["default","vip"]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

const schema = z.object({
  GroupRatio: z.string(),
  TopupGroupRatio: z.string(),
  UserGroups: z.string(),
  UserUsableGroups: z.string(),
  GroupGroupRatio: z.string(),
  AutoGroups: z.string(),
  MaxTokenAutoGroups: positiveIntegerSchema('Enter a positive integer'),
  DefaultUseAutoGroup: z.boolean(),
  GroupSpecialUsableGroup: z.string(),
})

function Fixture(props: {
  onSave?: (values: typeof defaults) => Promise<void>
  initial?: Partial<typeof defaults>
  legacyUserGroupNames?: string[]
  isSaving?: boolean
}) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const form = useForm({
    defaultValues: { ...defaults, ...props.initial },
    resolver: zodResolver(schema),
  })
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GroupRatioForm
        form={form}
        legacyUserGroupNames={props.legacyUserGroupNames ?? []}
        onSave={props.onSave ?? (async () => {})}
        isSaving={props.isSaving ?? false}
      />
    </SettingsPageProvider>
  )
}

describe('group settings workspace', () => {
  it('filters by description and clears search without discarding edits', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    await user.click(screen.getByRole('tab', { name: 'User groups' }))
    const search = screen.getByRole('textbox', {
      name: 'Search groups by name or description',
    })
    await user.type(search, 'PREMIUM')
    expect(
      screen.getAllByRole('textbox', { name: 'User group name' })
    ).toHaveLength(1)
    expect(
      screen.getByRole('textbox', { name: 'User group name' })
    ).toHaveValue('vip')
    await user.clear(screen.getByRole('spinbutton', { name: 'Top-up ratio' }))
    await user.type(
      screen.getByRole('spinbutton', { name: 'Top-up ratio' }),
      '0.6'
    )
    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(
      screen.getAllByRole('textbox', { name: 'User group name' })
    ).toHaveLength(2)
    expect(
      screen.getAllByRole('spinbutton', { name: 'Top-up ratio' })[1]
    ).toHaveValue(0.6)
    await user.type(search, 'missing')
    expect(screen.getByText('No results found')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    expect(search).toHaveValue('')
    expect(screen.getByDisplayValue('user_group_1')).toBeVisible()
  })

  it('keeps the default group fixed and blocks saving duplicate names', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'User groups' }))

    const groupNames = screen.getAllByRole('textbox', {
      name: 'User group name',
    })
    const secondGroupName = groupNames[1]
    if (!secondGroupName) throw new Error('Expected an editable user group')
    expect(groupNames[0]).toBeDisabled()
    await user.clear(secondGroupName)
    await user.type(secondGroupName, 'default')

    expect(screen.getByText('Duplicate group names: default')).toBeVisible()
    const saveButton = screen.getByRole('button', {
      name: 'Save group settings',
    })
    expect(saveButton).toBeDisabled()
    expect(onSave).not.toHaveBeenCalled()

    await user.clear(secondGroupName)
    await user.type(secondGroupName, 'vip')
    expect(saveButton).toBeEnabled()
    await user.click(saveButton)
    await waitFor(() => expect(onSave).toHaveBeenCalled())
  })

  it('blocks saving a user group with a blank name', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'User groups' }))

    const groupNames = screen.getAllByRole('textbox', {
      name: 'User group name',
    })
    const secondGroupName = groupNames[1]
    if (!secondGroupName) throw new Error('Expected an editable user group')
    await user.clear(secondGroupName)

    const saveButton = screen.getByRole('button', {
      name: 'Save group settings',
    })
    expect(saveButton).toBeDisabled()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('blocks reserved and overlong user group names but accepts the length limit', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'User groups' }))

    const groupName = screen.getAllByRole('textbox', {
      name: 'User group name',
    })[1]
    if (!groupName) throw new Error('Expected an editable user group')
    const saveButton = screen.getByRole('button', {
      name: 'Save group settings',
    })

    fireEvent.change(groupName, { target: { value: 'auto' } })
    expect(
      screen.getByText('The user group name "auto" is reserved.')
    ).toBeVisible()
    expect(saveButton).toBeDisabled()

    fireEvent.change(groupName, { target: { value: 'a'.repeat(65) } })
    expect(
      screen.getByText('User group names must be 64 characters or fewer.')
    ).toBeVisible()
    expect(saveButton).toBeDisabled()

    fireEvent.change(groupName, { target: { value: 'a'.repeat(64) } })
    expect(saveButton).toBeEnabled()
    await user.click(saveButton)
    await waitFor(() => expect(onSave).toHaveBeenCalled())
  })

  it('blocks negative top-up ratios while allowing zero', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'User groups' }))

    const ratios = screen.getAllByRole('spinbutton', { name: 'Top-up ratio' })
    const ratio = ratios[1]
    if (!ratio) throw new Error('Expected an editable top-up ratio')
    fireEvent.change(ratio, { target: { value: '-0.5' } })
    expect(
      screen.getByText('Top-up ratio must be zero or greater')
    ).toBeVisible()
    const saveButton = screen.getByRole('button', {
      name: 'Save group settings',
    })
    expect(saveButton).toBeDisabled()
    expect(onSave).not.toHaveBeenCalled()

    fireEvent.change(ratio, { target: { value: '0' } })
    expect(saveButton).toBeEnabled()
    await user.click(saveButton)
    await waitFor(() => expect(onSave).toHaveBeenCalled())
  })

  it('adds legacy user group names when the directory request finishes', async () => {
    const initial = { UserGroups: '{"default":"Standard account"}' }
    const view = render(<Fixture initial={initial} legacyUserGroupNames={[]} />)
    await userEvent
      .setup()
      .click(screen.getByRole('tab', { name: 'User groups' }))
    expect(
      screen.getAllByRole('textbox', { name: 'User group name' })
    ).toHaveLength(1)

    view.rerender(
      <Fixture initial={initial} legacyUserGroupNames={['old_user_group']} />
    )

    expect(await screen.findByDisplayValue('old_user_group')).toBeVisible()
  })

  it('keeps visibility rules in user groups and separates their source and target options', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        initial={{
          GroupRatio: '{"default":1,"vip":0.8,"channel_only":0.5}',
          UserGroups:
            '{"default":"Standard account","vip":"Premium account","user_only":"Enterprise account"}',
          GroupSpecialUsableGroup: '{"vip":{"+:default":"Standard"}}',
        }}
      />
    )
    await user.click(screen.getByRole('tab', { name: 'User groups' }))
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    const channelGroupSelect = screen.getAllByRole('combobox', {
      name: 'Channel group',
    })[1]
    await user.click(channelGroupSelect)
    expect(screen.getByRole('option', { name: 'channel_only' })).toBeVisible()
    expect(
      screen.queryByRole('option', { name: 'user_only' })
    ).not.toBeInTheDocument()
    await user.keyboard('{Escape}')

    const addUserGroupSelect = screen.getByRole('combobox', {
      name: 'Add rules for a user group',
    })
    await user.click(addUserGroupSelect)
    expect(screen.getByRole('option', { name: 'user_only' })).toBeVisible()
    expect(
      screen.queryByRole('option', { name: 'channel_only' })
    ).not.toBeInTheDocument()
    await user.keyboard('{Escape}')

    const toggle = screen.getByRole('button', { name: 'Rules for vip' })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('adds legacy same-name channel visibility without overriding explicit rules', () => {
    const migrated = migrateLegacyUserGroupVisibility({
      legacyCompatibility: true,
      legacyUserGroupNames: ['vip', 'default'],
      userGroups: '{"vip":"Premium","default":"Standard"}',
      groupRatio: '{"default":1,"vip":0.8}',
      userUsableGroups: '{}',
      groupSpecialUsableGroup:
        '{"vip":{"+:premium":"Premium"},"default":{"-:default":"remove"}}',
    })

    expect(JSON.parse(migrated ?? '{}')).toEqual({
      vip: { '+:premium': 'Premium', '+:vip': '用户分组' },
      default: { '-:default': 'remove' },
    })
  })

  it('does not restore a deleted legacy user group during visibility migration', () => {
    const migrated = migrateLegacyUserGroupVisibility({
      legacyCompatibility: true,
      legacyUserGroupNames: ['retired', 'vip'],
      userGroups: '{"vip":"Premium"}',
      groupRatio: '{"retired":0.5,"vip":0.8}',
      userUsableGroups: '{}',
      groupSpecialUsableGroup: '{}',
    })

    expect(JSON.parse(migrated ?? '{}')).toEqual({
      vip: { '+:vip': '用户分组' },
    })
  })

  it('reorders and removes auto groups with named controls and saves across sections', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Move vip up' }))
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent('vip')
    await user.click(
      screen.getByRole('switch', { name: 'Default to auto groups' })
    )
    await user.click(screen.getByRole('button', { name: 'Remove default' }))
    expect(screen.getByRole('button', { name: 'Move vip up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        AutoGroups: '[\n  "vip"\n]',
        DefaultUseAutoGroup: true,
      })
    )
  })

  it('reorders from the drag handle with arrow keys, preserves unknown groups and saves the new order', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          AutoGroups: '["default","vip","retired"]',
          MaxTokenAutoGroups: 1,
        }}
      />
    )
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent(
      'default'
    )
    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}')
    expect(within(list).getAllByRole('listitem')[2]).toHaveTextContent(
      'default'
    )
    expect(handle).toHaveFocus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[1]).toHaveTextContent(
      'default'
    )
    expect(screen.getByText('Not in pricing table')).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].AutoGroups)).toEqual([
      'vip',
      'default',
      'retired',
    ])
  })

  it('keeps a single auto group unchanged when using its drag handle keyboard controls', async () => {
    const user = userEvent.setup()
    render(<Fixture initial={{ AutoGroups: '["default"]' }} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}{ArrowDown}')
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(1)
    expect(within(list).getByRole('listitem')).toHaveTextContent('default')
    expect(handle).toHaveFocus()
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Move default down' })
    ).toBeDisabled()
  })

  it('opens the auto section and focuses its invalid limit when saving from another section', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    fireEvent.change(
      screen.getByRole('spinbutton', {
        name: 'Maximum custom groups per token',
      }),
      { target: { value: '0' } }
    )
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('tab', { name: 'Auto group order' })
      ).toHaveAttribute('aria-selected', 'true')
    )
    const input = screen.getByRole('spinbutton', {
      name: 'Maximum custom groups per token',
    })
    expect(input).toHaveAttribute('aria-invalid', 'true')
    await waitFor(() => expect(input).toHaveFocus())
    expect(screen.getByText('Enter a positive integer')).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('shows empty state guidance and disables save during a pending request', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        isSaving
        initial={{
          GroupRatio: '{}',
          TopupGroupRatio: '{}',
          UserUsableGroups: '{}',
          AutoGroups: '[]',
        }}
      />
    )
    expect(
      screen.getByText('No groups yet. Add a group to get started.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    await user.click(screen.getByRole('tab', { name: 'Special ratio rules' }))
    expect(
      screen.getByText('Base group ratios apply until you add an override.')
    ).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    expect(screen.getByText('No auto groups configured')).toBeVisible()
  })

  it('preserves pricing edits when switching between visual and JSON editors', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    await user.clear(
      screen.getAllByRole('textbox', { name: 'Group description' })[1]
    )
    await user.type(
      screen.getAllByRole('textbox', { name: 'Group description' })[1],
      'Updated description'
    )
    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
    expect(
      screen.getAllByRole('textbox', { name: 'Group description' })[1]
    ).toHaveValue('Updated description')
  })
})
