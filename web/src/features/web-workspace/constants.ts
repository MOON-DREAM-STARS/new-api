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
import type { WebProjectPermit } from './types'

export const WEB_WORKSPACE_ROUTE_PATH = '/web-workspace'

export const WEB_WORKSPACE_CONFIG_QUERY_KEY = [
  'web-workspace',
  'config',
] as const
export const WEB_WORKSPACE_STATUS_QUERY_KEY = [
  'web-workspace',
  'status',
] as const
export const WEB_WORKSPACE_SESSION_QUERY_KEY = [
  'web-workspace',
  'session',
] as const
export const WEB_WORKSPACE_PROJECTS_QUERY_KEY = [
  'web-workspace',
  'projects',
] as const

/** The capability probe is cheap; keep it fresh for a few minutes. */
export const WEB_WORKSPACE_CONFIG_STALE_TIME_MS = 5 * 60 * 1000

/** Session polling interval while the page is visible. */
export const WEB_WORKSPACE_SESSION_POLL_INTERVAL_MS = 5000

/**
 * Keep-alive interval while the tab is hidden: the display stream is detached,
 * so the control plane has to keep the idle runtime alive on its own.
 */
export const WEB_WORKSPACE_HIDDEN_ACTIVITY_INTERVAL_MS = 60000

/** Below this viewport width the remote surface is explicitly unsupported. */
export const WEB_WORKSPACE_DESKTOP_MIN_WIDTH_PX = 1024

/** Reconnect policy: capped exponential backoff with a hard attempt limit. */
export const WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS = 5
export const WEB_WORKSPACE_RECONNECT_BASE_DELAY_MS = 1000
export const WEB_WORKSPACE_RECONNECT_MAX_DELAY_MS = 15000

/** Runtime states where the stream endpoint is expected to accept an attach. */
export const WEB_WORKSPACE_LIVE_SESSION_STATES = [
  'STARTING',
  'RUNNING',
  'IDLE',
  'STOPPING',
] as const

/** Error codes emitted by the Web Workspace API. */
export const WEB_WORKSPACE_ERROR_CODES = {
  unauthenticated: 'WEB_WORKSPACE_UNAUTHENTICATED',
  entitlementDenied: 'WEB_WORKSPACE_ENTITLEMENT_DENIED',
  resourceNotFound: 'WEB_WORKSPACE_RESOURCE_NOT_FOUND',
  invalidRequest: 'WEB_WORKSPACE_INVALID_REQUEST',
  internalError: 'WEB_WORKSPACE_INTERNAL_ERROR',
  agentUnavailable: 'WEB_WORKSPACE_AGENT_UNAVAILABLE',
  sessionNotFound: 'WEB_WORKSPACE_SESSION_NOT_FOUND',
  sessionRequired: 'WEB_WORKSPACE_SESSION_REQUIRED',
  projectLimit: 'WEB_WORKSPACE_PROJECT_LIMIT',
  ticketInvalid: 'WEB_WORKSPACE_TICKET_INVALID',
} as const

/** i18n keys (English source strings) for each runtime state label. */
export const WEB_WORKSPACE_SESSION_STATE_LABEL_KEYS: Record<string, string> = {
  STARTING: 'Starting',
  RUNNING: 'Running',
  IDLE: 'Idle',
  STOPPING: 'Stopping',
  STOPPED: 'Stopped',
  FAILED: 'Failed',
}

/** i18n keys explaining an entitlement denial reason. */
export const WEB_WORKSPACE_DENIAL_REASON_KEYS: Record<string, string> = {
  global_disabled: 'Web Workspace is disabled by the administrator.',
  user_disabled: 'Web Workspace is disabled for your account.',
  role: 'Your account role does not include Web Workspace access.',
  group: 'Your account group does not include Web Workspace access.',
  unauthenticated: 'Sign in to use Web Workspace.',
}

/** A permit is unusable once its advertised expiry has passed. */
export function isPermitExpired(
  permit: WebProjectPermit | null,
  nowMs: number
): boolean {
  if (!permit) return false
  return permit.expires_at * 1000 <= nowMs
}
