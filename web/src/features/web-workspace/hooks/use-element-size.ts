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
import { useEffect, useState, type RefObject } from 'react'

export type ElementSize = {
  width: number
  height: number
}

/**
 * Tracks the content box of an element with a ResizeObserver.
 *
 * The workspace viewport uses this to recompute the presentation crop on every
 * host resize instead of measuring once on mount.
 */
export function useElementSize(
  ref: RefObject<HTMLElement | null>
): ElementSize | null {
  const [size, setSize] = useState<ElementSize | null>(null)

  useEffect(() => {
    const element = ref.current
    if (!element) return undefined

    const measure = () => {
      const rect = element.getBoundingClientRect()
      const width = Math.round(rect.width)
      const height = Math.round(rect.height)
      setSize((previous) => {
        if (
          previous &&
          previous.width === width &&
          previous.height === height
        ) {
          return previous
        }
        return { width, height }
      })
    }

    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    window.addEventListener('resize', measure)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', measure)
    }
  }, [ref])

  return size
}
