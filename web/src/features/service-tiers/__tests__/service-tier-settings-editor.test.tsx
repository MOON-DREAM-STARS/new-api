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

import { ServiceTierSettingsEditor } from '../components/service-tier-settings-editor'
import type { ServiceTierSettings } from '../types'

const initial: ServiceTierSettings = {
  enabled_tiers: ['economy', 'balanced', 'performance'],
  preference: 'lowest',
  allow_escalation: false,
  max_escalation_tier: '',
  version: 0,
}

function Fixture(props: {
  scope: 'account' | 'key'
  settings?: ServiceTierSettings
}) {
  const [settings, setSettings] = useState(props.settings ?? initial)
  return (
    <>
      <ServiceTierSettingsEditor
        scope={props.scope}
        settings={settings}
        ratios={{ economy: { min: 0.15, max: 0.15 } }}
        onChange={setSettings}
      />
      <output aria-label='Settings'>{JSON.stringify(settings)}</output>
    </>
  )
}

describe('service tier settings editor', () => {
  it('tells the account scope that keys without their own setting follow it', () => {
    render(<Fixture scope='account' />)

    expect(
      screen.getByText(
        'Where each request starts. API keys without their own tier setting follow this preference.'
      )
    ).toBeInTheDocument()
  })

  it('describes the preference as the start of this key in the key scope', () => {
    render(<Fixture scope='key' />)

    expect(
      screen.getByText('Where each request of this API key starts.')
    ).toBeInTheDocument()
  })

  it('reports a safe draft when a tier is switched off', async () => {
    const user = userEvent.setup()
    render(<Fixture scope='key' />)

    await user.click(screen.getByRole('switch', { name: 'Economy tier' }))

    expect(screen.getByLabelText('Settings')).toHaveTextContent(
      '"enabled_tiers":["balanced","performance"]'
    )
  })

  it('asks the user to choose again when the preferred tier is off', () => {
    render(
      <Fixture
        scope='key'
        settings={{
          ...initial,
          enabled_tiers: ['economy', 'performance'],
          preference: 'balanced',
        }}
      />
    )

    expect(screen.getByRole('alert')).toHaveTextContent(
      'The preferred tier is turned off. Turn it on or choose another preference'
    )
  })
})
