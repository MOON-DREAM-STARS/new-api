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
import { useCallback, useEffect, useState } from 'react'

export type ImmersiveMode = {
  immersive: boolean
  enter: () => void
  exit: () => void
  toggle: () => void
}

export const WEB_WORKSPACE_IMMERSIVE_ATTRIBUTE = 'webWorkspaceImmersive'

/**
 * Workspace immersive mode.
 *
 * This is a workspace layout state, not the Fullscreen API: the global header,
 * the icon rail and the project rail step aside so the remote browser keeps
 * almost the whole viewport. `Esc` exits and the page always keeps a visible
 * exit control; the Fullscreen API stays an independent enhancement.
 */
export function useImmersiveMode(): ImmersiveMode {
  const [immersive, setImmersive] = useState(false)

  const exit = useCallback(() => setImmersive(false), [])
  const enter = useCallback(() => setImmersive(true), [])
  const toggle = useCallback(() => setImmersive((value) => !value), [])

  useEffect(() => {
    const root = document.documentElement
    if (!immersive) {
      delete root.dataset[WEB_WORKSPACE_IMMERSIVE_ATTRIBUTE]
      root.style.removeProperty('--app-header-height')
      return undefined
    }

    root.dataset[WEB_WORKSPACE_IMMERSIVE_ATTRIBUTE] = 'true'
    root.style.setProperty('--app-header-height', '0px')

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setImmersive(false)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      delete root.dataset[WEB_WORKSPACE_IMMERSIVE_ATTRIBUTE]
      root.style.removeProperty('--app-header-height')
    }
  }, [immersive])

  return { immersive, enter, exit, toggle }
}
