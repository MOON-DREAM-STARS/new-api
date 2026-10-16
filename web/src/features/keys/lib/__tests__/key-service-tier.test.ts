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
import { describe, expect, test } from 'vitest'

import type { ServiceTierSettings } from '@/features/service-tiers/types'

import {
  displayedTierSettings,
  keyTierPayload,
  resultingTierOverride,
  type KeyTierChoice,
} from '../key-service-tier'

const account: ServiceTierSettings = {
  enabled_tiers: ['economy', 'balanced', 'performance'],
  preference: 'lowest',
  allow_escalation: false,
  max_escalation_tier: '',
  version: 4,
  is_default: true,
}
const own: ServiceTierSettings = {
  enabled_tiers: ['performance'],
  preference: 'performance',
  allow_escalation: false,
  max_escalation_tier: '',
  version: 9,
}
const custom = (settings: ServiceTierSettings): KeyTierChoice => ({
  kind: 'custom',
  settings,
})

describe('displayedTierSettings', () => {
  test('shows the account default for a key without settings of its own', () => {
    expect(displayedTierSettings(null, null, account)).toBe(account)
  })

  test("shows the key's own settings when the page was left alone", () => {
    expect(displayedTierSettings(null, own, account)).toBe(own)
  })

  test('shows the account default after the user chose to follow it', () => {
    expect(displayedTierSettings({ kind: 'follow' }, own, account)).toBe(
      account
    )
  })

  test('shows the settings the user edited', () => {
    const edited = { ...account, preference: 'balanced' as const }
    expect(displayedTierSettings(custom(edited), null, account)).toBe(edited)
  })
})

describe('resultingTierOverride', () => {
  test('is none for a new key that is left alone', () => {
    expect(resultingTierOverride(null, null, account)).toBeNull()
  })

  test('stays none when a switch is changed and put back', () => {
    const putBack = { ...account, enabled_tiers: [...account.enabled_tiers] }
    expect(resultingTierOverride(custom(putBack), null, account)).toBeNull()
  })

  test('is none after following the account again', () => {
    expect(resultingTierOverride({ kind: 'follow' }, own, account)).toBeNull()
  })
})

describe('keyTierPayload', () => {
  test('sends nothing for a new key whose page was left alone', () => {
    expect(keyTierPayload(null, null, account)).toEqual({ changed: false })
  })

  test('sends the four settings of a new key that differ from the account', () => {
    const edited: ServiceTierSettings = {
      ...account,
      enabled_tiers: ['economy', 'performance'],
      preference: 'performance',
    }
    expect(keyTierPayload(custom(edited), null, account)).toEqual({
      changed: true,
      value: {
        enabled_tiers: ['economy', 'performance'],
        preference: 'performance',
        allow_escalation: false,
        max_escalation_tier: '',
      },
    })
  })

  test('sends null when a key with its own settings follows the account again', () => {
    expect(keyTierPayload({ kind: 'follow' }, own, account)).toEqual({
      changed: true,
      value: null,
    })
  })

  test('sends nothing when a key that follows the account chooses to follow it', () => {
    expect(keyTierPayload({ kind: 'follow' }, null, account)).toEqual({
      changed: false,
    })
  })

  test("sends nothing when the settings still match the key's own", () => {
    expect(keyTierPayload(custom({ ...own }), own, account)).toEqual({
      changed: false,
    })
  })

  test('keeps explicit settings that equal the account default when the key had its own', () => {
    const payload = keyTierPayload(custom({ ...account }), own, account)
    expect(payload).toMatchObject({
      changed: true,
      value: { preference: 'lowest' },
    })
  })
})
