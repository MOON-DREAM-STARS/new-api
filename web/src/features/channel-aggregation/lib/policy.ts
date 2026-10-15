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

import { SERVICE_TIERS } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierKey } from '@/features/service-tiers/types'

import type {
  AggregationErrorCategory,
  AggregationPolicyConfig,
  AggregationTierChannel,
} from '../types'

export const ERROR_CATEGORIES: readonly AggregationErrorCategory[] = [
  'rate_limited',
  'server_error',
  'timeout',
  'channel_error',
]

export function errorCategoryLabel(t: TFunction, category: string): string {
  switch (category) {
    case 'rate_limited':
      return t('Rate limited (429)')
    case 'server_error':
      return t('Upstream server error (5xx)')
    case 'timeout':
      return t('Connection failure or request timeout (408)')
    case 'channel_error':
      return t('Channel credential or permission error (401/403)')
    case 'client_error':
      return t('Request error (4xx)')
    case 'local_rejection':
      return t('Rejected locally')
    case 'invalid_response':
      return t('Invalid upstream response')
    default:
      return category
  }
}

export function cloneConfig(
  config: AggregationPolicyConfig
): AggregationPolicyConfig {
  return structuredClone(config)
}

/** Fills tiers a stored config may lack, so every tier can be edited. */
export function completeConfig(
  config: AggregationPolicyConfig,
  template: AggregationPolicyConfig
): AggregationPolicyConfig {
  const result = cloneConfig(config)
  for (const tier of SERVICE_TIERS) {
    if (!result.tiers[tier]) {
      result.tiers[tier] = cloneConfig(template).tiers[tier]
    }
    result.tiers[tier].channels ??= []
  }
  result.strategy.retry_on ??= []
  result.strategy.user_groups ??= []
  return result
}

/**
 * The order the backend tries a pool in: higher priority first; equal
 * priorities share traffic by weight.
 */
export function sortPool(
  entries: AggregationTierChannel[]
): AggregationTierChannel[] {
  return [...entries].sort((a, b) =>
    a.priority === b.priority ? b.weight - a.weight : b.priority - a.priority
  )
}

export type PoolPosition = { level: number; share: number }

/** Priority level (1 = tried first) and traffic share of enabled entries. */
export function poolPositions(
  entries: AggregationTierChannel[]
): Map<number, PoolPosition> {
  const enabled = entries.filter((entry) => entry.enabled)
  const priorities = [...new Set(enabled.map((entry) => entry.priority))].sort(
    (a, b) => b - a
  )
  const positions = new Map<number, PoolPosition>()
  priorities.forEach((priority, index) => {
    const level = enabled.filter((entry) => entry.priority === priority)
    const total = level.reduce((sum, entry) => sum + entry.weight, 0)
    for (const entry of level) {
      positions.set(entry.channel_id, {
        level: index + 1,
        share: total > 0 ? entry.weight / total : 1 / level.length,
      })
    }
  })
  return positions
}

export type DraftProblem = { key: string; params?: Record<string, unknown> }

/**
 * Client-side checks that mirror the server's blocking rules so problems show
 * before saving. The server validates again and stays authoritative.
 */
export function policyProblems(
  config: AggregationPolicyConfig,
  enabled: boolean
): DraftProblem[] {
  const problems: DraftProblem[] = []
  let previous: { tier: ServiceTierKey; ratio: number } | null = null
  let enabledTiers = 0
  for (const tier of SERVICE_TIERS) {
    const value = config.tiers[tier]
    if (!(value.ratio > 0 && value.ratio <= 1000)) {
      problems.push({
        key: 'The ratio of tier {{tier}} must be greater than 0 and at most 1000',
        params: { tier },
      })
      continue
    }
    if (!value.enabled) continue
    enabledTiers++
    if (enabled && !value.channels.some((entry) => entry.enabled)) {
      problems.push({
        key: 'Enabled tier {{tier}} needs at least one enabled channel',
        params: { tier },
      })
    }
    if (previous && value.ratio < previous.ratio) {
      problems.push({
        key: 'Tier ratios must not decrease from economy to performance ({{tier}} is cheaper than {{previous}})',
        params: { tier, previous: previous.tier },
      })
    }
    previous = { tier, ratio: value.ratio }
  }
  if (enabled && enabledTiers === 0) {
    problems.push({ key: 'Enable at least one tier before enabling the rule' })
  }
  const strategy = config.strategy
  if (!(strategy.max_attempts >= 1 && strategy.max_attempts <= 10)) {
    problems.push({ key: 'Max attempts must be between 1 and 10' })
  }
  if (
    !(strategy.max_total_seconds >= 0 && strategy.max_total_seconds <= 3600)
  ) {
    problems.push({
      key: 'Max total duration must be between 0 and 3600 seconds',
    })
  }
  if (!(strategy.cooldown_failures >= 0 && strategy.cooldown_failures <= 20)) {
    problems.push({ key: 'Cooldown failure count must be between 0 and 20' })
  }
  if (
    strategy.cooldown_failures > 0 &&
    !(strategy.cooldown_seconds >= 1 && strategy.cooldown_seconds <= 3600)
  ) {
    problems.push({
      key: 'Cooldown duration must be between 1 and 3600 seconds',
    })
  }
  return problems
}

export function sameConfig(
  a: AggregationPolicyConfig,
  b: AggregationPolicyConfig
): boolean {
  return (
    JSON.stringify(normalizeForCompare(a)) ===
    JSON.stringify(normalizeForCompare(b))
  )
}

function normalizeForCompare(config: AggregationPolicyConfig) {
  return {
    tiers: SERVICE_TIERS.map((tier) => ({
      ...config.tiers[tier],
      channels: sortPool(config.tiers[tier].channels),
    })),
    strategy: {
      ...config.strategy,
      retry_on: [...config.strategy.retry_on].sort(),
      user_groups: [...config.strategy.user_groups].sort(),
    },
  }
}

/** Channel status codes of the channels table. */
export const CHANNEL_STATUS_ENABLED = 1
export const CHANNEL_STATUS_MANUALLY_DISABLED = 2
export const CHANNEL_STATUS_AUTO_DISABLED = 3

export function channelStatusLabel(t: TFunction, status: number): string {
  if (status === CHANNEL_STATUS_ENABLED) return t('Enabled')
  if (status === CHANNEL_STATUS_MANUALLY_DISABLED) return t('Manually disabled')
  if (status === CHANNEL_STATUS_AUTO_DISABLED) {
    return t('Automatically disabled')
  }
  return t('Unknown')
}
