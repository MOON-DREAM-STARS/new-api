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
import type { PriceConfig } from '@/features/price-comparison/types'

/** Where a proposal comes from; a mapping wins over a hand pick, and so on. */
export type ModelsDevSource = 'mapping' | 'selected' | 'site' | 'common'

export type ModelsDevStatus =
  | 'new'
  | 'update'
  | 'unchanged'
  | 'locked'
  | 'invalid'

export type ModelsDevLockReason =
  | 'explicit'
  | 'modified'
  | 'configured'
  | 'default'

/** A proposal that is written only after somebody approved it. */
export type ModelsDevReview = 'large_change' | 'reseller'

export type ModelsDevTrigger = 'release' | 'manual'

export interface ModelsDevConfig {
  auto_sync_enabled: boolean
  include_site_models: boolean
  include_common_models: boolean
  selected_keys: string[]
  excluded_keys: string[]
  mappings: Record<string, string>
  locked_models: string[]
  unlocked_models: string[]
}

export interface ModelsDevSummary {
  matched: number
  created: number
  updated: number
  unchanged: number
  locked: number
  skipped_review: number
  invalid: number
  unmatched: number
}

export interface ModelsDevStateView {
  last_sync_at: number
  last_sync_version: string
  last_trigger: ModelsDevTrigger | ''
  last_summary: ModelsDevSummary
  last_error?: string
  last_error_at?: number
  managed_count: number
  undo_count: number
  undo_at?: number
}

export interface ModelsDevOverview {
  config: ModelsDevConfig
  state: ModelsDevStateView
  source_url: string
  current_version: string
  /** True when the next scheduler pass imports. */
  auto_sync_due: boolean
}

/** One priced text model of one provider; prices are USD per 1M tokens. */
export interface ModelsDevEntry {
  key: string
  provider_id: string
  provider_name: string
  model_id: string
  normalized_id: string
  model_name: string
  release_date: string
  official: boolean
  common: boolean
  input: number
  output: number
  cache_read?: number
  cache_write?: number
  tiers?: { size: number; input: number; output: number }[]
}

export interface ModelsDevCatalogItem extends ModelsDevEntry {
  selected: boolean
  excluded: boolean
  /** Models of this site that the entry prices by name. */
  site_models?: string[]
  mapped_from?: string[]
}

export interface ModelsDevProviderFacet {
  id: string
  name: string
  count: number
}

export interface ModelsDevCatalogPage {
  total: number
  items: ModelsDevCatalogItem[]
  providers: ModelsDevProviderFacet[]
  fetched_at: number
  hash: string
}

export interface ModelsDevCatalogParams {
  q?: string
  provider?: string
  common?: boolean
  official?: boolean
  limit?: number
  offset?: number
  refresh?: boolean
}

/** The proposal for one model name of this site. */
export interface ModelsDevRow {
  model: string
  key?: string
  source: ModelsDevSource
  provider_id?: string
  provider_name?: string
  official?: boolean
  status: ModelsDevStatus
  lock_reason?: ModelsDevLockReason
  review?: ModelsDevReview
  /** New blended price over the one the last import wrote. */
  change?: number
  input: number
  output: number
  cache_read?: number
  cache_write?: number
  tiers?: number
  expression?: string
  version: string
  current: PriceConfig
  error?: string
}

export interface ModelsDevPlan {
  hash: string
  catalog_hash: string
  fetched_at: number
  rows: ModelsDevRow[]
  unmatched: string[]
  summary: ModelsDevSummary
}

export interface ModelsDevSyncRequest {
  plan_hash?: string
  /** Leave out to write every row that needs no decision. */
  models?: string[]
  approved?: string[]
  refresh?: boolean
}

export interface ModelsDevSyncResult {
  at: number
  summary: ModelsDevSummary
  written: string[]
  skipped_review: string[]
}

export interface ModelsDevUndoResult {
  restored: string[]
  conflicts: string[]
}

export interface ModelsDevExpressionResult {
  entry: ModelsDevEntry
  expression: string
}
