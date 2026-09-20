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
  mapCaretToAnchor,
  resolveLocalInputKey,
  type LocalInputKeyEvent,
} from '../local-input'

function keyEvent(
  key: string,
  modifiers: Partial<LocalInputKeyEvent> = {}
): LocalInputKeyEvent {
  return {
    key,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    isComposing: false,
    ...modifiers,
  }
}

describe('resolveLocalInputKey', () => {
  test('forwards the supported command and navigation keys', () => {
    expect(resolveLocalInputKey(keyEvent('Enter'))).toEqual({
      key: 'Enter',
      modifiers: [],
    })
    expect(resolveLocalInputKey(keyEvent('Tab', { shiftKey: true }))).toEqual({
      key: 'Tab',
      modifiers: ['shift'],
    })
    expect(resolveLocalInputKey(keyEvent('a', { ctrlKey: true }))).toEqual({
      key: 'a',
      modifiers: ['ctrl'],
    })
    expect(resolveLocalInputKey(keyEvent('Left'))).toEqual({
      key: 'ArrowLeft',
      modifiers: [],
    })
  })

  test('does not forward plain printable input or an active composition', () => {
    expect(resolveLocalInputKey(keyEvent('a'))).toBeNull()
    expect(resolveLocalInputKey(keyEvent('Enter', { isComposing: true }))).toBeNull()
  })

  test('allows ctrl/meta clipboard letters for bridge fallback', () => {
    expect(resolveLocalInputKey(keyEvent('c', { ctrlKey: true }))).toEqual({
      key: 'c',
      modifiers: ['ctrl'],
    })
    expect(resolveLocalInputKey(keyEvent('v', { metaKey: true }))).toEqual({
      key: 'v',
      modifiers: ['meta'],
    })
  })
})

describe('mapCaretToAnchor', () => {
  test('maps remote pixels through the real canvas scale', () => {
    expect(
      mapCaretToAnchor(
        { x: 100, y: 50, width: 1, height: 18 },
        {
          left: 20,
          top: 10,
          width: 800,
          height: 450,
          canvasWidth: 1600,
          canvasHeight: 900,
        }
      )
    ).toEqual({ left: 70, top: 35 })
  })

  test('returns null for missing caret or invalid canvas geometry', () => {
    expect(mapCaretToAnchor(null, null)).toBeNull()
    expect(
      mapCaretToAnchor(
        { x: 1, y: 1, width: 1, height: 1 },
        {
          left: 0,
          top: 0,
          width: 100,
          height: 100,
          canvasWidth: 0,
          canvasHeight: 100,
        }
      )
    ).toBeNull()
  })
})
