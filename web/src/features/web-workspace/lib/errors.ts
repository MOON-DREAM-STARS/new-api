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
import { WEB_WORKSPACE_ERROR_CODES } from '../constants'

export type WebWorkspaceErrorKind =
  | 'session_required'
  | 'project_limit'
  | 'project_creation_in_progress'
  | 'project_deletion_failed'
  | 'capacity_reached'
  | 'agent_unavailable'
  | 'resource_removed'
  | 'forbidden'
  | 'unknown'

export type WebWorkspaceErrorInfo = {
  kind: WebWorkspaceErrorKind
  /** i18n key (English source string) for the operator-facing explanation. */
  messageKey: string
  code: string | null
  status: number | null
}

type ErrorPayload = {
  code: string | null
  status: number | null
  reason: string | null
}

/**
 * i18n keys for every actionable failure class. They stay English source
 * strings so the locale files can translate them in place.
 */
export const WEB_WORKSPACE_ERROR_MESSAGE_KEYS: Record<
  WebWorkspaceErrorKind,
  string
> = {
  session_required: 'Start a browser session before creating a project.',
  project_limit:
    'You have reached the project limit. Remove an existing project first.',
  project_creation_in_progress:
    'A project creation is already running. Finish it or wait for it to fail before starting another.',
  project_deletion_failed:
    'The provider project could not be deleted. Web Workspace kept the local registration.',
  capacity_reached:
    'The server already has an active Web Workspace. Wait for it to become idle or stop it before starting another.',
  agent_unavailable:
    'The browser agent is unavailable right now. Try again in a moment.',
  resource_removed: 'This resource is unavailable or has been removed.',
  forbidden: 'Web Workspace access is not available for this account.',
  unknown: 'Something went wrong. Please try again.',
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== 'object' || value === null) return null
  return value as Record<string, unknown>
}

function readString(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null
}

function readNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/** Reads the New API error envelope out of an axios failure. */
export function readWebWorkspaceErrorPayload(error: unknown): ErrorPayload {
  const root = asRecord(error)
  const response = asRecord(root?.response)
  const data = asRecord(response?.data)
  return {
    code: readString(data?.code),
    status: readNumber(response?.status),
    reason: readString(data?.reason),
  }
}

function classifyKind(
  code: string | null,
  status: number | null
): WebWorkspaceErrorKind {
  switch (code) {
    case WEB_WORKSPACE_ERROR_CODES.sessionRequired:
      return 'session_required'
    case WEB_WORKSPACE_ERROR_CODES.projectLimit:
      return 'project_limit'
    case WEB_WORKSPACE_ERROR_CODES.projectCreationInProgress:
      return 'project_creation_in_progress'
    case WEB_WORKSPACE_ERROR_CODES.projectDeletionRejected:
      return 'project_deletion_failed'
    case WEB_WORKSPACE_ERROR_CODES.projectDeletionTimeout:
    case WEB_WORKSPACE_ERROR_CODES.projectDeletionUnavailable:
      return 'agent_unavailable'
    case WEB_WORKSPACE_ERROR_CODES.capacityReached:
      return 'capacity_reached'
    case WEB_WORKSPACE_ERROR_CODES.agentUnavailable:
    case 'WEB_WORKSPACE_NAVIGATION_TIMEOUT':
    case 'WEB_WORKSPACE_NAVIGATION_UNAVAILABLE':
      return 'agent_unavailable'
    case WEB_WORKSPACE_ERROR_CODES.invalidRequest:
      return 'unknown'
    case WEB_WORKSPACE_ERROR_CODES.resourceNotFound:
    case WEB_WORKSPACE_ERROR_CODES.sessionNotFound:
    case WEB_WORKSPACE_ERROR_CODES.ticketInvalid:
      return 'resource_removed'
    case WEB_WORKSPACE_ERROR_CODES.entitlementDenied:
    case WEB_WORKSPACE_ERROR_CODES.unauthenticated:
      return 'forbidden'
    default:
      break
  }

  if (status === 403) return 'forbidden'
  if (status === 404) return 'resource_removed'
  if (status === 503) return 'agent_unavailable'
  return 'unknown'
}

/** Classifies a failure so the UI can show actionable guidance instead of a bare error. */
export function classifyWebWorkspaceError(
  error: unknown
): WebWorkspaceErrorInfo {
  const payload = readWebWorkspaceErrorPayload(error)
  const kind = classifyKind(payload.code, payload.status)
  return {
    kind,
    messageKey: WEB_WORKSPACE_ERROR_MESSAGE_KEYS[kind],
    code: payload.code,
    status: payload.status,
  }
}

/** Entitlement reason from a denial payload, if present. */
export function readWebWorkspaceDenialReason(error: unknown): string | null {
  return readWebWorkspaceErrorPayload(error).reason
}

/** Policy-class failures mean the resource must not be treated as usable. */
export function isPolicyDenied(info: WebWorkspaceErrorInfo): boolean {
  return info.kind === 'resource_removed' || info.kind === 'forbidden'
}
