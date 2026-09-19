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
/**
 * Abbreviates a project name for the 64px project rail. The full name stays
 * available through the tooltip and the accessible name, so nothing is lost.
 */
export function projectAbbreviation(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return '?'
  const words = trimmed.split(/[\s_-]+/u).filter(Boolean)
  const first = words[0]
  const second = words[1]
  if (first && second) {
    return (first[0] + second[0]).toUpperCase()
  }
  return [...trimmed].slice(0, 2).join('').toUpperCase()
}
