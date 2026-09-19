import type { RemoteSurfaceStatus } from '../hooks/use-remote-surface'
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
import { isLiveSessionState } from './session'

/**
 * Presentation state of the workspace connection. It is always derived from
 * the real session state and the real stream status; nothing here is set by
 * the page itself.
 */
export type WorkspaceConnection =
  | 'connected'
  | 'connecting'
  | 'reconnecting'
  | 'failed'
  | 'idle'

export const CONNECTION_DOT_CLASSES: Record<WorkspaceConnection, string> = {
  connected: 'bg-emerald-500',
  connecting: 'bg-amber-500 animate-pulse motion-reduce:animate-none',
  reconnecting: 'bg-amber-500 animate-pulse motion-reduce:animate-none',
  failed: 'bg-destructive',
  idle: 'bg-muted-foreground/40',
}

export const CONNECTION_LABEL_KEYS: Record<WorkspaceConnection, string> = {
  connected: 'Connected',
  connecting: 'Connecting',
  reconnecting: 'Reconnecting',
  failed: 'Disconnected',
  idle: 'No browser session is running.',
}

/** Combines the control-plane session and the renderer status. */
export function resolveConnection(input: {
  sessionState: string | undefined
  surfaceStatus: RemoteSurfaceStatus
  surfaceEnabled: boolean
}): WorkspaceConnection {
  if (input.sessionState === 'STARTING') return 'connecting'
  if (!isLiveSessionState(input.sessionState)) return 'idle'
  if (!input.surfaceEnabled) return 'idle'
  if (input.surfaceStatus === 'connected') return 'connected'
  if (input.surfaceStatus === 'failed') return 'failed'
  if (input.surfaceStatus === 'reconnecting') return 'reconnecting'
  return 'connecting'
}
