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
 * Capability probe returned by `GET /api/web-workspace/config`.
 *
 * `enabled` reflects the global switch; `entitled` reflects the caller's
 * own entitlement. Menu visibility is never a permission — every API call is
 * still enforced on the server.
 */
export type WebWorkspaceConfig = {
  enabled: boolean
  entitled: boolean
}

/**
 * Client-side rendering state derived from the capability probe.
 *
 * - `ready`: the feature is enabled and the caller is entitled.
 * - `disabled`: the feature is switched off globally.
 * - `not-entitled`: the feature is on but this account has no access.
 */
export type WebWorkspaceAccessState = 'ready' | 'disabled' | 'not-entitled'

/** Session states reported by the Browser Agent runtime. */
export type WebWorkspaceSessionState =
  | 'STARTING'
  | 'RUNNING'
  | 'IDLE'
  | 'STOPPING'
  | 'STOPPED'
  | 'FAILED'

/** Commands accepted by the control plane for the remote browser history. */
export type WebWorkspaceNavigationAction =
  | 'back'
  | 'forward'
  | 'reload'
  | 'state'

/** Navigation capability reported by the control plane. No URL is exposed. */
export type WebWorkspaceNavigation = {
  can_go_back: boolean
  can_go_forward: boolean
  updated_at: number
}

/** Runtime policy mode reported by the control plane. Never client supplied. */
export type WebWorkspaceSessionMode = 'LOCKED' | 'LOGIN'

/** URL-free health state for the page inside the remote browser. */
export type WebWorkspacePage = {
  state: 'READY' | 'RETRYING' | 'FAILED'
  error: string
  attempts: number
  updated_at: number
}

/** URL-free state for the provider-side project creation operation. */
export type WebWorkspaceProjectCreation = {
  permit_id: string
  state: 'RUNNING' | 'CREATED' | 'FAILED'
  error: string
  updated_at: number
}

/** Control-plane view of one browser session. Runtime ids never reach the client. */
export type WebWorkspaceSession = {
  session_id: string
  state: WebWorkspaceSessionState | string
  mode: WebWorkspaceSessionMode | string
  created_at: number
  last_seen_at: number
  idle_deadline_at: number
  stream_bytes_out: number
  stream_bytes_in: number
  navigation: WebWorkspaceNavigation | null
  page: WebWorkspacePage | null
  project_creation: WebWorkspaceProjectCreation | null
}

export type WebWorkspaceStatus = {
  entitled: boolean
  reason?: string
  workspace?: {
    provider: string
    status: number
    created_at: number
    last_active_at: number
  }
}

/** Registered project mapping. The provider-side external id is never returned. */
export type WebProject = {
  id: number
  provider: string
  name: string
  created_at: number
  updated_at: number
}

/**
 * Short-lived permit for exactly one provider-side project creation. It does
 * not register a project by itself; the guard observation does.
 */
export type WebProjectPermit = {
  permit_id: string
  expires_at: number
}

/** Single-use stream ticket for one WSS attach. */
export type WebWorkspaceStreamTicket = {
  ticket: string
  expires_at: number
  stream_url: string
}

/** Standard New API envelope. */
export type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data: T
}
