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

import { resolveDenialReasonKey, resolveWebWorkspaceAccess } from '../access'

describe('resolveWebWorkspaceAccess', () => {
  test('returns disabled when the feature is switched off', () => {
    expect(resolveWebWorkspaceAccess({ enabled: false, entitled: false })).toBe(
      'disabled'
    )
    expect(resolveWebWorkspaceAccess({ enabled: false, entitled: true })).toBe(
      'disabled'
    )
  })

  test('returns not-entitled when the feature is on but the account has no access', () => {
    expect(resolveWebWorkspaceAccess({ enabled: true, entitled: false })).toBe(
      'not-entitled'
    )
  })

  test('returns ready only when the feature is enabled and the account is entitled', () => {
    expect(resolveWebWorkspaceAccess({ enabled: true, entitled: true })).toBe(
      'ready'
    )
  })
})

describe('resolveDenialReasonKey', () => {
  test('maps every known denial reason to its explanation key', () => {
    expect(resolveDenialReasonKey('role')).toBe(
      'Your account role does not include Web Workspace access.'
    )
    expect(resolveDenialReasonKey('group')).toBe(
      'Your account group does not include Web Workspace access.'
    )
    expect(resolveDenialReasonKey('user_disabled')).toBe(
      'Web Workspace is disabled for your account.'
    )
    expect(resolveDenialReasonKey('global_disabled')).toBe(
      'Web Workspace is disabled by the administrator.'
    )
  })

  test('returns null for unknown or missing reasons', () => {
    expect(resolveDenialReasonKey('something_new')).toBeNull()
    expect(resolveDenialReasonKey(null)).toBeNull()
  })
})
