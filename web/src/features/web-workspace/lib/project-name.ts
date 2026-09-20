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

export const PROJECT_NAME_SEGMENT_MAX_LENGTH = 24

const PROJECT_NAME_SEGMENT_PATTERN = /^[\p{L}\p{N}_]+$/u

/** Trims one half of the `user-project` naming convention. */
export function normalizeProjectNameSegment(value: string): string {
  return value.trim()
}

/** True when one segment is safe to combine with the single separator. */
export function isValidProjectNameSegment(value: string): boolean {
  const segment = normalizeProjectNameSegment(value)
  const length = [...segment].length
  return (
    length > 0 &&
    length <= PROJECT_NAME_SEGMENT_MAX_LENGTH &&
    PROJECT_NAME_SEGMENT_PATTERN.test(segment)
  )
}

/** Combines the two UI fields into the only name sent to the API. */
export function composeProjectName(
  userName: string,
  projectName: string
): string {
  return `${normalizeProjectNameSegment(userName)}-${normalizeProjectNameSegment(projectName)}`
}

/** Splits a legacy or new name only when it already satisfies the same rule. */
export function splitProjectName(
  name: string
): { userName: string; projectName: string } | null {
  const trimmed = name.trim()
  const separator = trimmed.indexOf('-')
  if (separator <= 0 || separator !== trimmed.lastIndexOf('-')) return null
  const userName = trimmed.slice(0, separator)
  const projectName = trimmed.slice(separator + 1)
  if (
    !isValidProjectNameSegment(userName) ||
    !isValidProjectNameSegment(projectName)
  ) {
    return null
  }
  return { userName, projectName }
}
