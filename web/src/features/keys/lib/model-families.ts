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
import type { UserModelFamilies } from '../types'

/** The id the backend uses for models that belong to no family. */
export const UNCLASSIFIED_FAMILY_ID = 0

/**
 * The ids a key can be limited to: every family, plus the group of models
 * without a family when the user has such models or the key already includes
 * that group.
 */
export function selectableFamilyIds(
  data: UserModelFamilies,
  storedIds: readonly number[] = []
): number[] {
  const ids = data.families.map((family) => family.id)
  const hasUnclassified =
    data.unclassified_models.length > 0 ||
    storedIds.includes(UNCLASSIFIED_FAMILY_ID)
  if (hasUnclassified) ids.push(UNCLASSIFIED_FAMILY_ID)
  return ids
}

/**
 * The selection the wizard starts with: everything for a key without a limit,
 * otherwise the stored limit without families that no longer exist.
 */
export function initialFamilySelection(
  data: UserModelFamilies,
  storedIds: readonly number[]
): ReadonlySet<number> {
  const selectable = selectableFamilyIds(data, storedIds)
  if (storedIds.length === 0) return new Set(selectable)
  return new Set(storedIds.filter((id) => selectable.includes(id)))
}

/**
 * The family limit to store for a selection. Selecting everything stores no
 * limit, so models added to any family later stay available to the key.
 */
export function familyLimitFromSelection(
  data: UserModelFamilies,
  storedIds: readonly number[],
  selection: ReadonlySet<number>
): number[] {
  const selectable = selectableFamilyIds(data, storedIds)
  const chosen = selectable.filter((id) => selection.has(id))
  if (chosen.length === selectable.length) return []
  // An empty list means "no limit", so an empty selection must never reach it.
  if (chosen.length === 0) {
    throw new Error('Select at least one model family')
  }
  return chosen.sort((a, b) => a - b)
}

export function sameFamilyLimit(
  a: readonly number[],
  b: readonly number[]
): boolean {
  return a.length === b.length && a.every((id) => b.includes(id))
}

/**
 * The names of the families a key is limited to, in the order of the family
 * list. Ids of families that no longer exist are skipped.
 */
export function familyLimitNames(
  ids: readonly number[],
  data: UserModelFamilies | undefined,
  unclassifiedName: string
): string[] {
  const names = (data?.families ?? [])
    .filter((family) => ids.includes(family.id))
    .map((family) => family.name)
  if (ids.includes(UNCLASSIFIED_FAMILY_ID)) names.push(unclassifiedName)
  return names
}
