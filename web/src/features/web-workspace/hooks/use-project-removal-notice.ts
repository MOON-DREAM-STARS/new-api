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
import { useCallback, useEffect, useRef, useState } from 'react'

import type { WebProject } from '../types'

export type ProjectRemovalNotice = {
  /** Names of projects that disappeared without a user-initiated delete. */
  removedNames: string[]
  /** Marks a delete the user already confirmed so it is not reported. */
  markExpectedRemoval: (projectId: number) => void
  /** Clears a delete marker when the provider rejects or no-ops the delete. */
  clearExpectedRemoval: (projectId: number) => void
  /** Records a policy-denied project reported by a mutation. */
  reportRemoved: (name: string) => void
  dismiss: () => void
}

/**
 * Detects projects that disappear between list refreshes — the guard removes
 * projects it can no longer observe (`project_not_found`), and the UI must
 * explain that instead of silently dropping them.
 */
export function useProjectRemovalNotice(
  projects: WebProject[] | undefined
): ProjectRemovalNotice {
  const expectedRemovalsRef = useRef<Set<number>>(new Set())
  const knownProjectsRef = useRef<Map<number, string> | null>(null)
  const [removedNames, setRemovedNames] = useState<string[]>([])

  useEffect(() => {
    if (!projects) return
    const known = knownProjectsRef.current
    const next = new Map(projects.map((project) => [project.id, project.name]))
    if (known) {
      const disappeared: string[] = []
      for (const [id, name] of known) {
        if (next.has(id)) continue
        if (expectedRemovalsRef.current.delete(id)) continue
        disappeared.push(name)
      }
      if (disappeared.length > 0) {
        setRemovedNames((current) => [...new Set([...current, ...disappeared])])
      }
    }
    knownProjectsRef.current = next
  }, [projects])

  const markExpectedRemoval = useCallback((projectId: number) => {
    expectedRemovalsRef.current.add(projectId)
  }, [])

  const clearExpectedRemoval = useCallback((projectId: number) => {
    expectedRemovalsRef.current.delete(projectId)
  }, [])

  const reportRemoved = useCallback((name: string) => {
    setRemovedNames((current) =>
      current.includes(name) ? current : [...current, name]
    )
  }, [])

  const dismiss = useCallback(() => {
    setRemovedNames([])
  }, [])

  return {
    removedNames,
    markExpectedRemoval,
    clearExpectedRemoval,
    reportRemoved,
    dismiss,
  }
}
