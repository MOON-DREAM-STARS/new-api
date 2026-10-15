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
  completeConfig,
  policyProblems,
  poolPositions,
  sameConfig,
  sortPool,
} from '../lib/policy'
import type { AggregationPolicyConfig } from '../types'

function config(): AggregationPolicyConfig {
  return {
    tiers: {
      economy: {
        enabled: true,
        ratio: 0.15,
        channels: [{ channel_id: 1, priority: 0, weight: 1, enabled: true }],
      },
      balanced: {
        enabled: true,
        ratio: 0.2,
        channels: [{ channel_id: 2, priority: 0, weight: 1, enabled: true }],
      },
      performance: {
        enabled: true,
        ratio: 0.4,
        channels: [{ channel_id: 3, priority: 0, weight: 1, enabled: true }],
      },
    },
    strategy: {
      same_tier_failover: true,
      max_attempts: 4,
      max_total_seconds: 120,
      retry_on: ['rate_limited', 'server_error'],
      skip_failed_channels: true,
      cooldown_failures: 3,
      cooldown_seconds: 60,
      user_groups: [],
    },
  }
}

describe('channel pool order', () => {
  const pool = [
    { channel_id: 1, priority: 0, weight: 5, enabled: true },
    { channel_id: 2, priority: 10, weight: 1, enabled: true },
    { channel_id: 3, priority: 0, weight: 15, enabled: true },
    { channel_id: 4, priority: 10, weight: 1, enabled: false },
  ]

  it('sorts by priority, then weight', () => {
    expect(sortPool(pool).map((entry) => entry.channel_id)).toEqual([
      2, 4, 3, 1,
    ])
  })

  it('reports levels and traffic shares of enabled channels only', () => {
    const positions = poolPositions(pool)
    expect(positions.get(2)).toEqual({ level: 1, share: 1 })
    expect(positions.get(3)).toEqual({ level: 2, share: 0.75 })
    expect(positions.get(1)).toEqual({ level: 2, share: 0.25 })
    expect(positions.has(4)).toBe(false)
  })

  it('splits evenly when every weight is zero', () => {
    const positions = poolPositions([
      { channel_id: 1, priority: 0, weight: 0, enabled: true },
      { channel_id: 2, priority: 0, weight: 0, enabled: true },
    ])
    expect(positions.get(1)?.share).toBe(0.5)
  })
})

describe('policy draft checks', () => {
  it('accepts the DeepSeek example', () => {
    expect(policyProblems(config(), true)).toEqual([])
  })

  it('rejects decreasing ratios and out-of-range values', () => {
    const draft = config()
    draft.tiers.balanced.ratio = 0.1
    draft.tiers.performance.ratio = 0
    draft.strategy.max_attempts = 11
    const keys = policyProblems(draft, false).map((problem) => problem.key)
    expect(keys).toContain(
      'Tier ratios must not decrease from economy to performance ({{tier}} is cheaper than {{previous}})'
    )
    expect(keys).toContain(
      'The ratio of tier {{tier}} must be greater than 0 and at most 1000'
    )
    expect(keys).toContain('Max attempts must be between 1 and 10')
  })

  it('ignores disabled tiers when checking ratio order', () => {
    const draft = config()
    draft.tiers.balanced.enabled = false
    draft.tiers.balanced.ratio = 0.01
    expect(policyProblems(draft, true)).toEqual([])
  })

  it('needs an enabled channel only when the rule is enabled', () => {
    const draft = config()
    draft.tiers.economy.channels[0].enabled = false
    expect(policyProblems(draft, false)).toEqual([])
    expect(policyProblems(draft, true)).toEqual([
      {
        key: 'Enabled tier {{tier}} needs at least one enabled channel',
        params: { tier: 'economy' },
      },
    ])
  })

  it('needs at least one tier for an enabled rule', () => {
    const draft = config()
    for (const tier of ['economy', 'balanced', 'performance'] as const) {
      draft.tiers[tier].enabled = false
    }
    expect(policyProblems(draft, true).map((problem) => problem.key)).toEqual([
      'Enable at least one tier before enabling the rule',
    ])
  })
})

describe('policy comparison', () => {
  it('treats reordered pools and error lists as unchanged', () => {
    const a = config()
    const b = config()
    b.tiers.economy.channels.push({
      channel_id: 9,
      priority: -1,
      weight: 1,
      enabled: true,
    })
    a.tiers.economy.channels.unshift({
      channel_id: 9,
      priority: -1,
      weight: 1,
      enabled: true,
    })
    b.strategy.retry_on = ['server_error', 'rate_limited']
    expect(sameConfig(a, b)).toBe(true)
    b.strategy.max_attempts = 2
    expect(sameConfig(a, b)).toBe(false)
  })

  it('fills tiers missing from a stored config', () => {
    const stored = config()
    const partial = {
      ...stored,
      tiers: { economy: stored.tiers.economy },
    } as AggregationPolicyConfig
    const completed = completeConfig(partial, config())
    expect(completed.tiers.performance.ratio).toBe(0.4)
    expect(partial.tiers.performance).toBeUndefined()
  })
})
