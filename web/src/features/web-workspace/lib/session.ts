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
import {
  WEB_WORKSPACE_LIVE_SESSION_STATES,
  WEB_WORKSPACE_SESSION_STATE_LABEL_KEYS,
} from '../constants'

/** True while the runtime is expected to accept a stream attach. */
export function isLiveSessionState(state: string | undefined): boolean {
  if (!state) return false
  return (WEB_WORKSPACE_LIVE_SESSION_STATES as readonly string[]).includes(
    state
  )
}

/** i18n key for a runtime state label, with a safe fallback for new states. */
export function sessionStateLabelKey(state: string | undefined): string {
  if (!state) return 'Unknown'
  return WEB_WORKSPACE_SESSION_STATE_LABEL_KEYS[state] ?? state
}

/** Seconds remaining until a Unix-second deadline; never negative. */
export function remainingSeconds(
  deadlineUnixSeconds: number,
  nowMs: number
): number {
  if (!deadlineUnixSeconds) return 0
  return Math.max(0, Math.ceil(deadlineUnixSeconds - nowMs / 1000))
}

/** `m:ss` countdown text; `0:00` once expired. */
export function formatCountdown(totalSeconds: number): string {
  const clamped = Math.max(0, Math.floor(totalSeconds))
  const minutes = Math.floor(clamped / 60)
  const seconds = clamped % 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}
