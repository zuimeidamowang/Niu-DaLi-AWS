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

import { WalletStatsCard } from '../wallet-stats-card'

describe('wallet statistics layout', () => {
  it('keeps statistics stacked on mobile before and after loading', () => {
    const { container, rerender } = render(
      <WalletStatsCard user={null} loading />
    )
    expect(container.firstElementChild).toHaveClass(
      'grid-cols-1',
      'sm:grid-cols-3'
    )

    rerender(<WalletStatsCard user={null} />)

    expect(container.firstElementChild).toHaveClass(
      'grid-cols-1',
      'sm:grid-cols-3'
    )
    expect(screen.getByText('Total requests made')).not.toHaveClass('hidden')
    expect(container.firstElementChild?.children).toHaveLength(3)
  })
})
