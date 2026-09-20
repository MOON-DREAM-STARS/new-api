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
import type {
  WebWorkspaceInputCaret,
  WebWorkspaceInputModifier,
} from '../types'

export type LocalCanvasMetrics = {
  left: number
  top: number
  width: number
  height: number
  canvasWidth: number
  canvasHeight: number
}

export type LocalInputKey = {
  key: string
  modifiers: WebWorkspaceInputModifier[]
}

export type LocalInputKeyEvent = Pick<
  KeyboardEvent,
  'key' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey' | 'isComposing'
>

const SPECIAL_KEYS: Record<string, string> = {
  Esc: 'Escape',
  Left: 'ArrowLeft',
  Right: 'ArrowRight',
  Up: 'ArrowUp',
  Down: 'ArrowDown',
}

const FORWARDED_SPECIAL_KEYS = new Set([
  'Enter',
  'Backspace',
  'Delete',
  'Escape',
  'Tab',
  'ArrowUp',
  'ArrowDown',
  'ArrowLeft',
  'ArrowRight',
  'Home',
  'End',
  'PageUp',
  'PageDown',
])

const CLIPBOARD_LETTERS = new Set(['a', 'c', 'v'])

function normalizeModifiers(
  event: LocalInputKeyEvent
): WebWorkspaceInputModifier[] {
  const modifiers: WebWorkspaceInputModifier[] = []
  if (event.altKey) modifiers.push('alt')
  if (event.ctrlKey) modifiers.push('ctrl')
  if (event.metaKey) modifiers.push('meta')
  if (event.shiftKey) modifiers.push('shift')
  return modifiers
}

/**
 * Resolves one local keydown into the frozen remote-key contract. Plain
 * printable keys are deliberately excluded: they are committed through the
 * local input element and injected as text instead.
 */
export function resolveLocalInputKey(
  event: LocalInputKeyEvent
): LocalInputKey | null {
  if (event.isComposing) return null
  const normalizedKey = SPECIAL_KEYS[event.key] ?? event.key
  const modifiers = normalizeModifiers(event)
  const hasCommandModifier = event.ctrlKey || event.metaKey

  if (CLIPBOARD_LETTERS.has(normalizedKey)) {
    if (!hasCommandModifier) return null
    return { key: normalizedKey, modifiers }
  }
  if (FORWARDED_SPECIAL_KEYS.has(normalizedKey)) {
    return { key: normalizedKey, modifiers }
  }
  return null
}

/**
 * Maps a remote caret rectangle into the frame-local canvas coordinate space.
 * The mapping uses the real canvas backing-store size, not a device-pixel guess.
 */
export function mapCaretToAnchor(
  caret: WebWorkspaceInputCaret | null | undefined,
  canvas: LocalCanvasMetrics | null | undefined
): { left: number; top: number } | null {
  if (!caret || !canvas || canvas.canvasWidth <= 0 || canvas.canvasHeight <= 0) {
    return null
  }
  return {
    left: canvas.left + caret.x * (canvas.width / canvas.canvasWidth),
    top: canvas.top + caret.y * (canvas.height / canvas.canvasHeight),
  }
}

/** Center fallback used when the remote page has no active caret. */
export function centerOfCanvas(
  canvas: LocalCanvasMetrics | null | undefined
): { left: number; top: number } | null {
  if (!canvas) return null
  return {
    left: canvas.left + canvas.width / 2,
    top: canvas.top + canvas.height / 2,
  }
}
