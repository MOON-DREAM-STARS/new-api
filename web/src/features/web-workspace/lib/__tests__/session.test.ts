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
  formatCountdown,
  isLiveSessionState,
  remainingSeconds,
  sessionStateLabelKey,
} from '../session'

describe('isLiveSessionState', () => {
  test('treats starting, running, idle and stopping as live', () => {
    for (const state of ['STARTING', 'RUNNING', 'IDLE', 'STOPPING']) {
      expect(isLiveSessionState(state)).toBe(true)
    }
  })

  test('treats stopped, failed, unknown and missing states as not live', () => {
    expect(isLiveSessionState('STOPPED')).toBe(false)
    expect(isLiveSessionState('FAILED')).toBe(false)
    expect(isLiveSessionState('SOMETHING_NEW')).toBe(false)
    expect(isLiveSessionState(undefined)).toBe(false)
  })
})

describe('sessionStateLabelKey', () => {
  test('maps known runtime states to their label keys', () => {
    expect(sessionStateLabelKey('RUNNING')).toBe('Running')
    expect(sessionStateLabelKey('IDLE')).toBe('Idle')
    expect(sessionStateLabelKey('FAILED')).toBe('Failed')
  })

  test('falls back for unknown states instead of hiding them', () => {
    expect(sessionStateLabelKey('RESTARTING')).toBe('RESTARTING')
    expect(sessionStateLabelKey(undefined)).toBe('Unknown')
  })
})

describe('remainingSeconds', () => {
  test('counts down from a unix-second deadline', () => {
    expect(remainingSeconds(1000, 998_000)).toBe(2)
  })

  test('never returns a negative value', () => {
    expect(remainingSeconds(1000, 1_500_000)).toBe(0)
    expect(remainingSeconds(0, 1_500_000)).toBe(0)
  })
})

describe('formatCountdown', () => {
  test('formats minutes and zero-padded seconds', () => {
    expect(formatCountdown(125)).toBe('2:05')
    expect(formatCountdown(9)).toBe('0:09')
    expect(formatCountdown(0)).toBe('0:00')
  })
})
