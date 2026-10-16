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
import {
  formatMoney,
  readListPrice,
  type TokenPrice,
} from '@/features/price-comparison/lib/prices'
import { toIntlLocale } from '@/i18n/languages'

import type {
  ModelsDevCatalogItem,
  ModelsDevConfig,
  ModelsDevPlan,
  ModelsDevRow,
  ModelsDevSyncRequest,
} from './types'

/**
 * USD per million tokens, the way models.dev quotes them: "$0.28". `language`
 * is the i18n language ("zhCN" is not a valid Intl locale, so it is converted).
 */
export function formatUsd(
  value: number | undefined,
  language?: string
): string {
  if (value === undefined || !Number.isFinite(value)) return '-'
  return formatMoney(value, 'USD', 1, toIntlLocale(language))
}

export interface CurrentPrice extends TokenPrice {
  /** The model bills by time of day; the price shown is the peak price. */
  timeBased: boolean
}

/** What the model costs now, or null when it has no per-token price. */
export function currentPriceOf(row: Pick<ModelsDevRow, 'current'>) {
  const list = readListPrice(row.current)
  return list ? { ...list.peak, timeBased: list.timeBased } : null
}

export function isWritable(row: Pick<ModelsDevRow, 'status'>): boolean {
  return row.status === 'new' || row.status === 'update'
}

/** The filters of the list of proposals. */
export type RowFilter = 'changes' | 'locked' | 'unchanged' | 'invalid'

export function rowFilterOf(row: Pick<ModelsDevRow, 'status'>): RowFilter {
  switch (row.status) {
    case 'new':
    case 'update':
      return 'changes'
    case 'locked':
      return 'locked'
    case 'invalid':
      return 'invalid'
    default:
      return 'unchanged'
  }
}

/** The filter to start with: the first one that has something to show. */
export function firstFilter(counts: Record<RowFilter, number>): RowFilter {
  if (counts.changes > 0) return 'changes'
  if (counts.locked > 0) return 'locked'
  if (counts.invalid > 0) return 'invalid'
  return 'unchanged'
}

/** The rows an import writes without anybody's decision. */
export function defaultChecked(plan: ModelsDevPlan): Set<string> {
  return new Set(
    plan.rows
      .filter((row) => isWritable(row) && !row.review)
      .map((row) => row.model)
  )
}

/**
 * What to send for the rows that are checked. Leaving out `models` when every
 * row that needs no decision is still checked makes the import a complete one
 * for this release; the rows the administrator approved go in `approved`.
 */
export function buildSyncRequest(
  plan: ModelsDevPlan,
  checked: ReadonlySet<string>
): ModelsDevSyncRequest {
  const writable = plan.rows.filter(isWritable)
  const chosen = writable.filter((row) => checked.has(row.model))
  const complete = writable
    .filter((row) => !row.review)
    .every((row) => checked.has(row.model))
  const approved = chosen.filter((row) => row.review).map((row) => row.model)
  const request: ModelsDevSyncRequest = { plan_hash: plan.hash }
  if (!complete) request.models = chosen.map((row) => row.model)
  if (approved.length > 0) request.approved = approved
  return request
}

/** The entries that were picked by hand and the ones left out. */
export interface EntryChoice {
  selected: ReadonlySet<string>
  excluded: ReadonlySet<string>
}

export function choiceOf(config: ModelsDevConfig): EntryChoice {
  return {
    selected: new Set(config.selected_keys),
    excluded: new Set(config.excluded_keys),
  }
}

/** Whether the switches of the configuration take the entry by themselves. */
export function isAutoIncluded(
  item: Pick<ModelsDevCatalogItem, 'common' | 'site_models'>,
  config: ModelsDevConfig
): boolean {
  return (
    (config.include_site_models && (item.site_models?.length ?? 0) > 0) ||
    (config.include_common_models && item.common)
  )
}

/** A hand pick always counts; an exclusion only removes automatic choices. */
export function isIncluded(
  item: Pick<ModelsDevCatalogItem, 'key' | 'common' | 'site_models'>,
  config: ModelsDevConfig,
  choice: EntryChoice
): boolean {
  if (choice.selected.has(item.key)) return true
  if (choice.excluded.has(item.key)) return false
  return isAutoIncluded(item, config)
}

export function setEntryIncluded(
  item: Pick<ModelsDevCatalogItem, 'key' | 'common' | 'site_models'>,
  config: ModelsDevConfig,
  choice: EntryChoice,
  include: boolean
): EntryChoice {
  const selected = new Set(choice.selected)
  const excluded = new Set(choice.excluded)
  const auto = isAutoIncluded(item, config)
  if (include) {
    excluded.delete(item.key)
    if (!auto) selected.add(item.key)
  } else {
    selected.delete(item.key)
    if (auto) excluded.add(item.key)
  }
  return { selected, excluded }
}

export function sameChoice(a: EntryChoice, b: EntryChoice): boolean {
  const same = (left: ReadonlySet<string>, right: ReadonlySet<string>) =>
    left.size === right.size && [...left].every((key) => right.has(key))
  return same(a.selected, b.selected) && same(a.excluded, b.excluded)
}

export function withChoice(
  config: ModelsDevConfig,
  choice: EntryChoice
): ModelsDevConfig {
  return {
    ...config,
    selected_keys: [...choice.selected].sort(),
    excluded_keys: [...choice.excluded].sort(),
  }
}

const without = (list: string[], name: string) =>
  list.filter((item) => item !== name)
const added = (list: string[], name: string) =>
  list.includes(name) ? list : [...list, name]

/** Lets the import write over a model that has a price. */
export function allowImport(
  config: ModelsDevConfig,
  model: string
): ModelsDevConfig {
  return {
    ...config,
    locked_models: without(config.locked_models, model),
    unlocked_models: added(config.unlocked_models, model),
  }
}

/** Keeps a model out of every import. */
export function keepOut(
  config: ModelsDevConfig,
  model: string
): ModelsDevConfig {
  return {
    ...config,
    unlocked_models: without(config.unlocked_models, model),
    locked_models: added(config.locked_models, model),
  }
}

/** Ties a model of this site to a models.dev entry; a null key removes it. */
export function withMapping(
  config: ModelsDevConfig,
  model: string,
  key: string | null
): ModelsDevConfig {
  const mappings = { ...config.mappings }
  if (key === null) delete mappings[model]
  else mappings[model] = key
  return { ...config, mappings }
}
