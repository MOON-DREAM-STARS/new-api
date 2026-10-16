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
import type { PriceComparisonSettings } from '../types'

/** The models chosen for one model family, in display order. */
export function picksOf(
  settings: PriceComparisonSettings,
  familyId: number
): string[] {
  return (
    settings.families.find((pick) => pick.family_id === familyId)?.models ?? []
  )
}

/**
 * Replaces the models of one model family, keeping its place in the list; a
 * family without models is dropped.
 */
export function withPicks(
  settings: PriceComparisonSettings,
  familyId: number,
  models: string[]
): PriceComparisonSettings {
  const exists = settings.families.some((pick) => pick.family_id === familyId)
  let families = settings.families.flatMap((pick) => {
    if (pick.family_id !== familyId) return [pick]
    return models.length > 0 ? [{ family_id: familyId, models }] : []
  })
  if (!exists && models.length > 0) {
    families = [...families, { family_id: familyId, models }]
  }
  return { ...settings, families }
}

/** Compares two settings by value, ignoring the order of the model families. */
export function sameSettings(
  a: PriceComparisonSettings,
  b: PriceComparisonSettings
): boolean {
  if (
    a.baseline_model !== b.baseline_model ||
    a.price_basis !== b.price_basis ||
    a.currency !== b.currency ||
    a.ratio_format !== b.ratio_format
  ) {
    return false
  }
  const ids = new Set([...a.families, ...b.families].map((p) => p.family_id))
  for (const id of ids) {
    const left = picksOf(a, id)
    const right = picksOf(b, id)
    if (left.length !== right.length) return false
    if (left.some((name, index) => name !== right[index])) return false
  }
  return true
}
