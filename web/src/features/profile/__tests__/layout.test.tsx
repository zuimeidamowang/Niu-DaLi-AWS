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
import { describe, expect, it } from 'vitest'

import { ProfileHeader } from '../components/profile-header'
import type { UserProfile } from '../types'

describe('profile header layout', () => {
  it('wraps long account names and badges while keeping mobile statistics readable', () => {
    const profile: UserProfile = {
      id: 42,
      username: 'enterprise-account',
      display_name: 'Enterprise account with a long display name',
      role: 1,
      group: 'default',
      quota: 1000000,
      used_quota: 0,
      request_count: 1234,
      status: 1,
      aff_count: 0,
      aff_quota: 0,
      aff_history_quota: 0,
      created_time: 0,
    }
    render(<ProfileHeader profile={profile} loading={false} />)

    const heading = screen.getByRole('heading', {
      name: profile.display_name,
    })
    expect(heading.parentElement).toHaveClass('flex-wrap')
    const requestDescription = screen.getByText('Total requests made')
    const requestCell = requestDescription.parentElement
    expect(requestCell?.parentElement).toHaveClass(
      'grid-cols-1',
      'sm:grid-cols-3'
    )
    expect(requestCell?.children[1]).toHaveClass('break-all')
    expect(requestDescription).not.toHaveClass('hidden')
  })
})
