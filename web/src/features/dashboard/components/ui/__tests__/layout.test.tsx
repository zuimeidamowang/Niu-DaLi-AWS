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
import { Activity } from 'lucide-react'
import { describe, expect, it, vi } from 'vitest'

import { Button } from '@/components/ui/button'

import { PanelWrapper } from '../panel-wrapper'
import { StatCard } from '../stat-card'

describe('dashboard card layout', () => {
  it('lets long statistic values wrap without widening their grid column', () => {
    const value = '123456789012345678901234567890'
    const { container } = render(
      <StatCard
        title='Requests'
        value={value}
        description='Request volume'
        icon={Activity}
        compactMobile
      />
    )

    expect(container.firstElementChild).toHaveClass('min-w-0')
    expect(screen.getByText(value)).toHaveClass('break-all')
    expect(screen.getByText('Requests')).toHaveClass('line-clamp-2')
  })

  it('wraps panel actions below long headings while keeping the action usable', async () => {
    const onRefresh = vi.fn()
    const user = userEvent.setup()
    const title = 'International platform service performance overview'
    render(
      <PanelWrapper
        title={title}
        headerActions={<Button onClick={onRefresh}>Refresh</Button>}
      >
        Latest performance results
      </PanelWrapper>
    )

    const heading = screen.getByText(title)
    expect(heading).toHaveClass('break-words')
    expect(heading.parentElement).toHaveClass('min-w-0', 'basis-40')
    expect(heading.parentElement?.parentElement).toHaveClass('flex-wrap')
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(onRefresh).toHaveBeenCalledOnce()
  })
})
