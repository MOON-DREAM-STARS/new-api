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
 * `@novnc/novnc` ships plain ESM without type declarations. Only the surface
 * the Web Workspace feature uses is declared here.
 */
declare module '@novnc/novnc' {
  export type RFBOptions = {
    shared?: boolean
    credentials?: { username?: string; password?: string; target?: string }
    repeaterID?: string
    wsProtocols?: string[]
    scaleViewport?: boolean
    resizeSession?: boolean
    clipViewport?: boolean
    viewOnly?: boolean
    focusOnClick?: boolean
    showDotCursor?: boolean
    background?: string
  }

  export default class RFB {
    constructor(
      target: Element,
      urlOrChannel: string | WebSocket,
      options?: RFBOptions
    )
    scaleViewport: boolean
    viewOnly: boolean
    focusOnClick: boolean
    disconnect: () => void
    focus: (options?: FocusOptions) => void
    addEventListener: (type: string, listener: (event: Event) => void) => void
    removeEventListener: (
      type: string,
      listener: (event: Event) => void
    ) => void
  }
}
