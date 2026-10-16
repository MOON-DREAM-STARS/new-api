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
import { sameSettings } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierSettings } from '@/features/service-tiers/types'

import type { ApiKeyServiceTierInput } from '../types'

/**
 * What the user did on the service tier page of the wizard. No choice means
 * the page was left alone and the key keeps what it has.
 */
export type KeyTierChoice =
  | { kind: 'custom'; settings: ServiceTierSettings }
  | { kind: 'follow' }

/** The settings the wizard shows: the choice, the key's own or the account's. */
export function displayedTierSettings(
  choice: KeyTierChoice | null,
  own: ServiceTierSettings | null,
  account: ServiceTierSettings
): ServiceTierSettings {
  if (choice?.kind === 'custom') return choice.settings
  if (choice?.kind === 'follow') return account
  return own ?? account
}

/**
 * The key's own settings after saving, or null when it follows the account.
 * Settings that match what the key uses now are not a change, so editing a
 * switch and putting it back stores nothing.
 */
export function resultingTierOverride(
  choice: KeyTierChoice | null,
  own: ServiceTierSettings | null,
  account: ServiceTierSettings
): ServiceTierSettings | null {
  if (choice?.kind === 'follow') return null
  if (choice?.kind === 'custom') {
    return sameSettings(choice.settings, own ?? account) ? own : choice.settings
  }
  return own
}

export function toTierInput(
  settings: ServiceTierSettings
): ApiKeyServiceTierInput {
  return {
    enabled_tiers: settings.enabled_tiers,
    preference: settings.preference,
    allow_escalation: settings.allow_escalation,
    max_escalation_tier: settings.max_escalation_tier,
  }
}

export type KeyTierPayload =
  | { changed: false }
  | { changed: true; value: ApiKeyServiceTierInput | null }

/** What to send for the key's own settings, if anything. */
export function keyTierPayload(
  choice: KeyTierChoice | null,
  own: ServiceTierSettings | null,
  account: ServiceTierSettings
): KeyTierPayload {
  const result = resultingTierOverride(choice, own, account)
  if (!result) return own ? { changed: true, value: null } : { changed: false }
  if (own && sameSettings(result, own)) return { changed: false }
  return { changed: true, value: toTierInput(result) }
}
