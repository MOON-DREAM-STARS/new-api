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
import { describe, expect, test } from 'vitest'

import { WEB_WORKSPACE_ERROR_CODES } from '../../constants'
import {
  classifyWebWorkspaceError,
  isPolicyDenied,
  readWebWorkspaceDenialReason,
} from '../errors'

function apiError(status: number, code: string, reason?: string) {
  return {
    response: {
      status,
      data: { success: false, code, reason },
    },
  }
}

describe('classifyWebWorkspaceError', () => {
  test('classifies a missing session as an actionable session error', () => {
    const info = classifyWebWorkspaceError(
      apiError(409, WEB_WORKSPACE_ERROR_CODES.sessionRequired)
    )
    expect(info.kind).toBe('session_required')
    expect(info.messageKey).toBe(
      'Start a browser session before creating a project.'
    )
  })

  test('classifies the project limit conflict', () => {
    const info = classifyWebWorkspaceError(
      apiError(409, WEB_WORKSPACE_ERROR_CODES.projectLimit)
    )
    expect(info.kind).toBe('project_limit')
  })

  test('classifies the global runtime capacity conflict', () => {
    const info = classifyWebWorkspaceError(
      apiError(409, WEB_WORKSPACE_ERROR_CODES.capacityReached)
    )
    expect(info.kind).toBe('capacity_reached')
    expect(info.messageKey).toContain('active Web Workspace')
  })

  test('classifies an unavailable browser agent, including a bare 503', () => {
    expect(
      classifyWebWorkspaceError(
        apiError(503, WEB_WORKSPACE_ERROR_CODES.agentUnavailable)
      ).kind
    ).toBe('agent_unavailable')
    expect(classifyWebWorkspaceError({ response: { status: 503 } }).kind).toBe(
      'agent_unavailable'
    )
  })

  test('classifies navigation failures without throwing for unknown codes', () => {
    expect(
      classifyWebWorkspaceError(
        apiError(400, WEB_WORKSPACE_ERROR_CODES.invalidRequest)
      ).kind
    ).toBe('unknown')
    expect(
      classifyWebWorkspaceError(
        apiError(504, 'WEB_WORKSPACE_NAVIGATION_TIMEOUT')
      ).kind
    ).toBe('agent_unavailable')
    expect(
      classifyWebWorkspaceError(
        apiError(409, 'WEB_WORKSPACE_NAVIGATION_UNAVAILABLE')
      ).kind
    ).toBe('agent_unavailable')
    expect(
      classifyWebWorkspaceError(apiError(418, 'WEB_WORKSPACE_UNKNOWN')).kind
    ).toBe('unknown')
  })

  test('classifies removed resources from 404 and policy codes', () => {
    expect(
      classifyWebWorkspaceError(
        apiError(404, WEB_WORKSPACE_ERROR_CODES.resourceNotFound)
      ).kind
    ).toBe('resource_removed')
    expect(
      classifyWebWorkspaceError(
        apiError(404, WEB_WORKSPACE_ERROR_CODES.sessionNotFound)
      ).kind
    ).toBe('resource_removed')
    expect(
      classifyWebWorkspaceError(
        apiError(403, WEB_WORKSPACE_ERROR_CODES.entitlementDenied)
      ).kind
    ).toBe('forbidden')
  })

  test('falls back to an unknown failure for unexpected errors', () => {
    const info = classifyWebWorkspaceError(new Error('boom'))
    expect(info.kind).toBe('unknown')
    expect(info.status).toBeNull()
  })
})

describe('isPolicyDenied', () => {
  test('treats removed and forbidden resources as policy denials', () => {
    expect(isPolicyDenied(classifyWebWorkspaceError(apiError(404, 'X')))).toBe(
      true
    )
    expect(isPolicyDenied(classifyWebWorkspaceError(apiError(403, 'X')))).toBe(
      true
    )
    expect(isPolicyDenied(classifyWebWorkspaceError(apiError(500, 'X')))).toBe(
      false
    )
  })
})

describe('readWebWorkspaceDenialReason', () => {
  test('reads the reason from an entitlement denial payload', () => {
    expect(
      readWebWorkspaceDenialReason(
        apiError(403, WEB_WORKSPACE_ERROR_CODES.entitlementDenied, 'role')
      )
    ).toBe('role')
  })

  test('returns null when no reason is present', () => {
    expect(readWebWorkspaceDenialReason(new Error('boom'))).toBeNull()
  })
})
