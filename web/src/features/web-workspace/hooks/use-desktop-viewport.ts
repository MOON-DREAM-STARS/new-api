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
import { useEffect, useState } from 'react'

import { WEB_WORKSPACE_DESKTOP_MIN_WIDTH_PX } from '../constants'

function buildQuery(): string {
  return `(min-width: ${WEB_WORKSPACE_DESKTOP_MIN_WIDTH_PX}px)`
}

function readIsDesktop(): boolean {
  if (
    typeof window === 'undefined' ||
    typeof window.matchMedia !== 'function'
  ) {
    return true
  }
  return window.matchMedia(buildQuery()).matches
}

/**
 * The remote surface is declared desktop-only. This reports whether the
 * viewport is wide enough to use it instead of silently degrading.
 */
export function useDesktopViewport(): boolean {
  const [isDesktop, setIsDesktop] = useState(readIsDesktop)

  useEffect(() => {
    const mediaQuery = window.matchMedia(buildQuery())
    const handleChange = (event: MediaQueryListEvent) => {
      setIsDesktop(event.matches)
    }
    setIsDesktop(mediaQuery.matches)
    mediaQuery.addEventListener('change', handleChange)
    return () => {
      mediaQuery.removeEventListener('change', handleChange)
    }
  }, [])

  return isDesktop
}
