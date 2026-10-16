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
import type { TFunction } from 'i18next'

import type {
  ServiceTierKey,
  ServiceTierPreference,
  ServiceTierSettings,
  ServiceTierUserOverview,
  TierRatioRange,
} from '../types'

/** Tiers from the cheapest level to the most expensive one. */
export const SERVICE_TIERS: readonly ServiceTierKey[] = [
  'economy',
  'balanced',
  'performance',
]

export const SERVICE_TIER_PREFERENCES: readonly ServiceTierPreference[] = [
  'lowest',
  'balanced',
  'performance',
]

export function tierLevel(tier: string): number {
  return SERVICE_TIERS.indexOf(tier as ServiceTierKey)
}

export function tierLabel(t: TFunction, tier: string): string {
  switch (tier) {
    case 'economy':
      return t('Economy tier')
    case 'balanced':
      return t('Balanced tier')
    case 'performance':
      return t('Performance tier')
    default:
      return tier
  }
}

export function tierDescription(t: TFunction, tier: ServiceTierKey): string {
  switch (tier) {
    case 'economy':
      return t('The lowest price, suited to everyday and batch work')
    case 'balanced':
      return t('A balance of price, speed and stability')
    case 'performance':
      return t('The most stable and fastest channels at a higher price')
  }
}

export function preferenceLabel(
  t: TFunction,
  preference: ServiceTierPreference
): string {
  switch (preference) {
    case 'lowest':
      return t('Lowest price first')
    case 'balanced':
      return t('Balanced first')
    case 'performance':
      return t('Performance first')
  }
}

export function preferenceDescription(
  t: TFunction,
  preference: ServiceTierPreference
): string {
  switch (preference) {
    case 'lowest':
      return t('Start from the cheapest tier you turned on')
    case 'balanced':
      return t('Start from the balanced tier')
    case 'performance':
      return t('Start from the performance tier')
  }
}

/** The tier a preference starts from before model availability. */
export function preferenceStartTier(
  settings: Pick<ServiceTierSettings, 'enabled_tiers' | 'preference'>
): ServiceTierKey | undefined {
  if (settings.preference === 'balanced') return 'balanced'
  if (settings.preference === 'performance') return 'performance'
  return SERVICE_TIERS.find((tier) => settings.enabled_tiers.includes(tier))
}

/** Tiers the user may allow escalation up to: turned on and above the start. */
export function escalationCeilingOptions(
  settings: Pick<ServiceTierSettings, 'enabled_tiers' | 'preference'>
): ServiceTierKey[] {
  const start = preferenceStartTier(settings)
  if (!start) return []
  return SERVICE_TIERS.filter(
    (tier) =>
      settings.enabled_tiers.includes(tier) &&
      tierLevel(tier) > tierLevel(start)
  )
}

/**
 * Applies a tier switch while keeping the draft safe: at least one tier stays
 * on, and an escalation ceiling that became invalid only ever moves down (or
 * escalation turns off). A preferred tier that was turned off is kept so the
 * page can ask the user to choose again instead of switching silently.
 */
export function toggleTier(
  settings: ServiceTierSettings,
  tier: ServiceTierKey,
  enabled: boolean
): ServiceTierSettings {
  const enabledTiers = SERVICE_TIERS.filter((key) =>
    key === tier ? enabled : settings.enabled_tiers.includes(key)
  )
  if (enabledTiers.length === 0) return settings
  return normalizeCeiling({ ...settings, enabled_tiers: enabledTiers })
}

/**
 * A newly allowed escalation starts at the lowest ceiling; an invalid ceiling
 * falls to the highest valid tier below it.
 */
export function normalizeCeiling(
  settings: ServiceTierSettings
): ServiceTierSettings {
  if (!settings.allow_escalation) {
    return { ...settings, max_escalation_tier: '' }
  }
  const options = escalationCeilingOptions(settings)
  const current = settings.max_escalation_tier
  if (current && options.includes(current)) return settings
  const below = current
    ? options.filter((tier) => tierLevel(tier) < tierLevel(current))
    : options.slice(0, 1)
  if (below.length === 0) {
    return { ...settings, allow_escalation: false, max_escalation_tier: '' }
  }
  return { ...settings, max_escalation_tier: below.at(-1) ?? '' }
}

/** The draft's blocking problem, as an i18n key, or null. */
export function draftProblem(settings: ServiceTierSettings): string | null {
  const start = preferenceStartTier(settings)
  if (!start || !settings.enabled_tiers.includes(start)) {
    return 'The preferred tier is turned off. Turn it on or choose another preference'
  }
  return null
}

export function sameSettings(
  a: ServiceTierSettings,
  b: ServiceTierSettings
): boolean {
  return (
    a.preference === b.preference &&
    a.allow_escalation === b.allow_escalation &&
    a.max_escalation_tier === b.max_escalation_tier &&
    a.enabled_tiers.length === b.enabled_tiers.length &&
    a.enabled_tiers.every((tier) => b.enabled_tiers.includes(tier))
  )
}

/**
 * Ratios are shown with up to four decimals; the shared number formatter
 * keeps two, which would misstate ratios such as 0.125.
 */
export function formatTierRatio(
  ratio: number | undefined,
  locale: string | undefined
): string {
  if (ratio == null || Number.isNaN(ratio)) return '-'
  return `×${new Intl.NumberFormat(locale, { maximumFractionDigits: 4 }).format(ratio)}`
}

/** The lowest and highest ratio each tier has across the user's families. */
export function tierRatioRanges(
  overview?: ServiceTierUserOverview
): TierRatioRange {
  const ranges: TierRatioRange = {}
  for (const family of overview?.preview.families ?? []) {
    for (const tier of SERVICE_TIERS) {
      const ratio = family.offered_tiers[tier]
      if (ratio == null) continue
      const current = ranges[tier]
      ranges[tier] = current
        ? {
            min: Math.min(current.min, ratio),
            max: Math.max(current.max, ratio),
          }
        : { min: ratio, max: ratio }
    }
  }
  return ranges
}
