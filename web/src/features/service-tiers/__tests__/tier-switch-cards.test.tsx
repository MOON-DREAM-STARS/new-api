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
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { TierSwitchCards } from '../components/tier-switch-cards'
import { toggleTier } from '../lib/tiers'
import type { ServiceTierSettings } from '../types'

function Fixture() {
  const [settings, setSettings] = useState<ServiceTierSettings>({
    enabled_tiers: ['economy', 'balanced'],
    preference: 'lowest',
    allow_escalation: false,
    max_escalation_tier: '',
    version: 0,
  })
  return (
    <>
      <TierSwitchCards
        enabledTiers={settings.enabled_tiers}
        ratios={{
          economy: { min: 0.15, max: 0.15 },
          balanced: { min: 0.2, max: 0.25 },
        }}
        onToggle={(tier, enabled) =>
          setSettings((current) => toggleTier(current, tier, enabled))
        }
      />
      <output aria-label='Enabled tiers'>
        {settings.enabled_tiers.join(',')}
      </output>
    </>
  )
}

describe('tier switch cards', () => {
  it('shows the ratio range and keeps the last tier switched on', async () => {
    const user = userEvent.setup()
    render(<Fixture />)

    expect(screen.getByText('×0.15')).toBeInTheDocument()
    expect(screen.getByText('×0.2 – ×0.25')).toBeInTheDocument()
    expect(screen.getByText('Not offered yet')).toBeInTheDocument()

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(3)
    await user.click(switches[0])
    expect(screen.getByLabelText('Enabled tiers')).toHaveTextContent('balanced')

    // The only remaining tier cannot be turned off.
    expect(switches[1]).toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByText('At least one tier must stay on')
    ).toBeInTheDocument()

    await user.click(switches[2])
    expect(screen.getByLabelText('Enabled tiers')).toHaveTextContent(
      'balanced,performance'
    )
    expect(switches[1]).not.toHaveAttribute('aria-disabled', 'true')
  })
})
