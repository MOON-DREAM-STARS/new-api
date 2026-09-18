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
import { useCallback, useEffect, useState, type RefObject } from 'react'

export type FullscreenControls = {
  isFullscreen: boolean
  enterFullscreen: () => void
  exitFullscreen: () => void
}

/**
 * Fullscreen API wrapper for the remote surface container. Esc exits through
 * the browser's native handling; this only tracks the resulting state.
 */
export function useFullscreen(
  target: RefObject<HTMLElement | null>
): FullscreenControls {
  const [isFullscreen, setIsFullscreen] = useState(false)

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === target.current)
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange)
    }
  }, [target])

  const enterFullscreen = useCallback(() => {
    const element = target.current
    if (!element || typeof element.requestFullscreen !== 'function') return
    element.requestFullscreen().catch(() => undefined)
  }, [target])

  const exitFullscreen = useCallback(() => {
    if (typeof document.exitFullscreen !== 'function') return
    document.exitFullscreen().catch(() => undefined)
  }, [])

  return { isFullscreen, enterFullscreen, exitFullscreen }
}
