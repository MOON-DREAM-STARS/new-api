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
import { useEffect, useRef } from 'react'

import { useSidebar } from '@/components/ui/sidebar'
import { useLayout } from '@/context/layout-provider'

/**
 * Compacts the global New API navigation to the icon rail while the workspace
 * is mounted, and hides it entirely in immersive mode.
 *
 * The rail keeps the real navigation: every icon still links to its real
 * route, only the presentation becomes compact. The operator preference is
 * captured once and restored on unmount so the workspace layout never leaks
 * into the rest of the application.
 */
export function useCompactSidebar(enabled = true, immersive = false): void {
  const sidebar = useSidebar()
  const layout = useLayout()
  const latest = useRef({ open: sidebar.open, collapsible: layout.collapsible })
  latest.current = { open: sidebar.open, collapsible: layout.collapsible }

  useEffect(() => {
    if (!enabled) return undefined
    const restore = { ...latest.current }
    return () => {
      layout.setCollapsible(restore.collapsible)
      sidebar.setOpen(restore.open)
    }
    // The operator preference is captured once per mount; re-running on state
    // changes would immediately undo the compact rail.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled])

  useEffect(() => {
    if (!enabled) return
    layout.setCollapsible(immersive ? 'offcanvas' : 'icon')
    sidebar.setOpen(false)
    // Only the mode changes matter here; the setters are recreated on every
    // layout render and must not retrigger the mode switch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, immersive])
}
