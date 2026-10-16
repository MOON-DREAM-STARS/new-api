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
import { describe, expect, it } from 'vitest'

import {
  draftProblem,
  escalationCeilingOptions,
  formatTierRatio,
  normalizeCeiling,
  preferenceStartTier,
  sameSettings,
  tierRatioRanges,
  toggleTier,
} from '../lib/tiers'
import type { ServiceTierSettings, ServiceTierUserOverview } from '../types'

function settings(
  patch: Partial<ServiceTierSettings> = {}
): ServiceTierSettings {
  return {
    enabled_tiers: ['economy', 'balanced', 'performance'],
    preference: 'lowest',
    allow_escalation: false,
    max_escalation_tier: '',
    version: 0,
    ...patch,
  }
}

describe('service tier draft rules', () => {
  it('starts the lowest preference at the cheapest enabled tier', () => {
    expect(preferenceStartTier(settings())).toBe('economy')
    expect(
      preferenceStartTier(
        settings({ enabled_tiers: ['balanced', 'performance'] })
      )
    ).toBe('balanced')
    expect(preferenceStartTier(settings({ preference: 'performance' }))).toBe(
      'performance'
    )
  })

  it('only offers escalation ceilings above the starting tier', () => {
    expect(escalationCeilingOptions(settings())).toEqual([
      'balanced',
      'performance',
    ])
    expect(
      escalationCeilingOptions(settings({ preference: 'balanced' }))
    ).toEqual(['performance'])
    expect(
      escalationCeilingOptions(settings({ preference: 'performance' }))
    ).toEqual([])
    expect(
      escalationCeilingOptions(
        settings({ enabled_tiers: ['economy', 'performance'] })
      )
    ).toEqual(['performance'])
  })

  it('never turns off the last enabled tier', () => {
    const only = settings({
      enabled_tiers: ['balanced'],
      preference: 'balanced',
    })
    expect(toggleTier(only, 'balanced', false)).toBe(only)
  })

  it('keeps tiers in canonical order when one is turned back on', () => {
    const next = toggleTier(
      settings({ enabled_tiers: ['performance'] }),
      'economy',
      true
    )
    expect(next.enabled_tiers).toEqual(['economy', 'performance'])
  })

  it('moves an invalid ceiling down, never up', () => {
    const escalating = settings({
      allow_escalation: true,
      max_escalation_tier: 'performance',
    })
    const next = toggleTier(escalating, 'performance', false)
    expect(next.allow_escalation).toBe(true)
    expect(next.max_escalation_tier).toBe('balanced')
  })

  it('turns escalation off when no ceiling below the old one is left', () => {
    const escalating = settings({
      enabled_tiers: ['economy', 'balanced'],
      allow_escalation: true,
      max_escalation_tier: 'balanced',
    })
    const next = toggleTier(escalating, 'balanced', false)
    expect(next.allow_escalation).toBe(false)
    expect(next.max_escalation_tier).toBe('')
  })

  it('starts a newly allowed escalation at the lowest ceiling', () => {
    const next = normalizeCeiling(settings({ allow_escalation: true }))
    expect(next.max_escalation_tier).toBe('balanced')
    expect(
      normalizeCeiling(settings({ max_escalation_tier: 'performance' }))
        .max_escalation_tier
    ).toBe('')
  })

  it('blocks saving when the preferred tier is turned off', () => {
    expect(draftProblem(settings())).toBeNull()
    expect(
      draftProblem(
        settings({
          preference: 'balanced',
          enabled_tiers: ['economy', 'performance'],
        })
      )
    ).not.toBeNull()
  })

  it('compares settings regardless of tier order', () => {
    expect(
      sameSettings(
        settings({ enabled_tiers: ['economy', 'performance'] }),
        settings({ enabled_tiers: ['performance', 'economy'], version: 3 })
      )
    ).toBe(true)
    expect(sameSettings(settings(), settings({ allow_escalation: true }))).toBe(
      false
    )
  })

  it('formats ratios with up to four decimals', () => {
    expect(formatTierRatio(0.125, 'en-US')).toBe('×0.125')
    expect(formatTierRatio(0.15, 'en-US')).toBe('×0.15')
    expect(formatTierRatio(undefined, 'en-US')).toBe('-')
  })
})

describe('tier ratio ranges', () => {
  const overview = (
    offered: Array<
      Partial<Record<'economy' | 'balanced' | 'performance', number>>
    >
  ): ServiceTierUserOverview => ({
    available: true,
    settings: settings(),
    preview: {
      chain: [],
      families: offered.map((offered_tiers, index) => ({
        family: `family-${index}`,
        offered_tiers,
        model_overrides: 0,
        preferred_tier: 'economy',
        preferred_offered: true,
        steps: [],
        ceiling_ratio: 0,
      })),
    },
  })

  it('has no range before the overview is loaded', () => {
    expect(tierRatioRanges(undefined)).toEqual({})
  })

  it('spans the lowest and highest ratio of each tier across families', () => {
    expect(
      tierRatioRanges(
        overview([
          { economy: 0.15, performance: 0.4 },
          { economy: 0.1, balanced: 0.2, performance: 0.5 },
        ])
      )
    ).toEqual({
      economy: { min: 0.1, max: 0.15 },
      balanced: { min: 0.2, max: 0.2 },
      performance: { min: 0.4, max: 0.5 },
    })
  })
})
