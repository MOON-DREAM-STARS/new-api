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
  WEB_WORKSPACE_RECONNECT_BASE_DELAY_MS,
  WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS,
  WEB_WORKSPACE_RECONNECT_MAX_DELAY_MS,
} from '../constants'

/**
 * Capped exponential backoff for the Nth reconnect attempt (1-based):
 * 1s, 2s, 4s, 8s, 15s, 15s, ...
 */
export function reconnectDelayMs(attempt: number): number {
  const exponent = Math.max(0, attempt - 1)
  const delay = WEB_WORKSPACE_RECONNECT_BASE_DELAY_MS * 2 ** exponent
  return Math.min(delay, WEB_WORKSPACE_RECONNECT_MAX_DELAY_MS)
}

/** Whether another automatic attempt is allowed after `attempts` failures. */
export function canRetryReconnect(attempts: number): boolean {
  return attempts < WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS
}
