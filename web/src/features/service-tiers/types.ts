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
export type ServiceTierKey = 'economy' | 'balanced' | 'performance'

export type ServiceTierPreference = 'lowest' | 'balanced' | 'performance'

export type ServiceTierSettings = {
  enabled_tiers: ServiceTierKey[]
  preference: ServiceTierPreference
  allow_escalation: boolean
  max_escalation_tier: ServiceTierKey | ''
  version: number
  is_default?: boolean
}

export type ServiceTierStep = {
  tier: ServiceTierKey
  ratio: number
  escalation: boolean
}

export type ServiceTierFamilyPreview = {
  family: string
  offered_tiers: Partial<Record<ServiceTierKey, number>>
  model_overrides: number
  preferred_tier: ServiceTierKey
  preferred_offered: boolean
  steps: ServiceTierStep[]
  ceiling_ratio: number
}

export type ServiceTierPreview = {
  chain: ServiceTierStep[]
  families: ServiceTierFamilyPreview[]
}

export type ServiceTierUserOverview = {
  available: boolean
  settings: ServiceTierSettings
  preview: ServiceTierPreview
}
