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
import type {
  ServiceTierKey,
  ServiceTierSettings,
} from '@/features/service-tiers/types'

export type AggregationTierChannel = {
  channel_id: number
  priority: number
  weight: number
  enabled: boolean
}

export type AggregationTierConfig = {
  enabled: boolean
  ratio: number
  channels: AggregationTierChannel[]
}

export type AggregationErrorCategory =
  | 'rate_limited'
  | 'server_error'
  | 'timeout'
  | 'channel_error'

export type AggregationStrategy = {
  same_tier_failover: boolean
  max_attempts: number
  max_total_seconds: number
  retry_on: AggregationErrorCategory[]
  skip_failed_channels: boolean
  cooldown_failures: number
  cooldown_seconds: number
  user_groups: string[]
}

export type AggregationPolicyConfig = {
  tiers: Record<ServiceTierKey, AggregationTierConfig>
  strategy: AggregationStrategy
}

export type ModelFamily = {
  id: number
  key: string
  name: string
  description: string
  sort_order: number
  match_patterns: string[]
  version: number
  members: string[]
  default_enabled: boolean
  override_count: number
}

export type AggregationModel = { name: string; family_id: number }

export type AggregationOverview = {
  tiers: ServiceTierKey[]
  error_categories: AggregationErrorCategory[]
  families: ModelFamily[]
  models: AggregationModel[]
  groups: string[]
}

export type AggregationPolicy = {
  id: number
  family_id: number
  model_name: string
  enabled: boolean
  version: number
  updated_at: number
  updated_by: number
  config: AggregationPolicyConfig
}

export type ChannelHealth = {
  recent_failures: number
  last_failure_at?: number
  last_status?: number
  last_category?: string
  last_error_code?: string
  last_success_at?: number
  cooldown_until?: number
}

export type AggregationChannel = {
  id: number
  name: string
  type: number
  status: number
  group: string
  tag?: string
  response_time: number
  test_time: number
  status_reason?: string
  status_time?: number
  supported_members: number
  supports_model: boolean
  health: ChannelHealth
}

export type FamilyDetail = {
  family: ModelFamily
  default: AggregationPolicy | null
  overrides: AggregationPolicy[]
  template: AggregationPolicyConfig
  channels: AggregationChannel[]
}

export type FamilyInput = {
  key: string
  name: string
  description: string
  sort_order: number
  match_patterns: string[]
  version: number
}

export type PolicyInput = {
  family_id: number
  model_name: string
  enabled: boolean
  config: AggregationPolicyConfig
  version: number
}

export type ServerMessage = { key: string; params?: Record<string, unknown> }

export type SimulationChannel = {
  channel_id: number
  name: string
  priority: number
  weight: number
  status: number
  eligible: boolean
  reason?: string
  level?: number
  probability?: number
}

export type SimulationStep = {
  tier: ServiceTierKey
  ratio: number
  escalation: boolean
  channels: SimulationChannel[]
}

export type Simulation = {
  model_name: string
  family_name?: string
  policy_scope?: 'family' | 'model' | 'draft'
  applies: boolean
  reason?: 'not_member' | 'policy_disabled' | 'group_not_applicable' | 'no_tier'
  settings: ServiceTierSettings
  preferred_tier?: ServiceTierKey
  ceiling_ratio?: number
  max_attempts?: number
  steps: SimulationStep[]
}

export type SimulationInput = {
  model_name: string
  user_group: string
  settings: Omit<ServiceTierSettings, 'version' | 'is_default'> & {
    version?: number
  }
  draft?: PolicyInput
}
