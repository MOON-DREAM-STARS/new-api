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
import type { WebProject } from '../types'

const LAST_PROJECT_STORAGE_PREFIX = 'web-workspace:last-project:'

function storageKey(userId: number | null): string | null {
  return userId && userId > 0 ? `${LAST_PROJECT_STORAGE_PREFIX}${userId}` : null
}

export function readLastProjectId(userId: number | null): number | null {
  const key = storageKey(userId)
  if (!key) return null
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null
    const parsed = Number(raw)
    return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null
  } catch {
    return null
  }
}

export function rememberLastProjectId(
  userId: number | null,
  projectId: number
): void {
  const key = storageKey(userId)
  if (!key || projectId <= 0) return
  try {
    window.localStorage.setItem(key, String(projectId))
  } catch {
    // Local persistence is a convenience; the project list remains authoritative.
  }
}

export function resolveAutoOpenProject(
  projects: WebProject[],
  lastProjectId: number | null
): WebProject | null {
  if (projects.length === 0) return null
  if (lastProjectId !== null) {
    const remembered = projects.find((project) => project.id === lastProjectId)
    if (remembered) return remembered
  }
  return projects[projects.length - 1] ?? null
}
