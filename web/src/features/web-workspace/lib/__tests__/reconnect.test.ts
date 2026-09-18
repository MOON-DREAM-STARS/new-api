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

import {
  WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS,
  WEB_WORKSPACE_RECONNECT_MAX_DELAY_MS,
} from '../../constants'
import { canRetryReconnect, reconnectDelayMs } from '../reconnect'

describe('reconnectDelayMs', () => {
  test('grows exponentially from the base delay', () => {
    expect(reconnectDelayMs(1)).toBe(1000)
    expect(reconnectDelayMs(2)).toBe(2000)
    expect(reconnectDelayMs(3)).toBe(4000)
  })

  test('caps the delay at the configured maximum', () => {
    expect(reconnectDelayMs(10)).toBe(WEB_WORKSPACE_RECONNECT_MAX_DELAY_MS)
  })
})

describe('canRetryReconnect', () => {
  test('allows attempts below the limit and rejects the limit itself', () => {
    expect(canRetryReconnect(0)).toBe(true)
    expect(canRetryReconnect(WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS - 1)).toBe(
      true
    )
    expect(canRetryReconnect(WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS)).toBe(false)
  })
})
