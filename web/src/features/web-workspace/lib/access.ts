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
import { WEB_WORKSPACE_DENIAL_REASON_KEYS } from '../constants'
import type { WebWorkspaceAccessState, WebWorkspaceConfig } from '../types'
import type { WebWorkspaceErrorInfo } from './errors'

/**
 * Derives the render state from the capability probe. Hidden navigation and a
 * disabled page are UX only — the server still enforces entitlement.
 */
export function resolveWebWorkspaceAccess(
  config: WebWorkspaceConfig
): WebWorkspaceAccessState {
  if (!config.enabled) return 'disabled'
  if (!config.entitled) return 'not-entitled'
  return 'ready'
}

/** i18n key for the reason returned by a `WEB_WORKSPACE_ENTITLEMENT_DENIED` denial. */
export function resolveDenialReasonKey(reason: string | null): string | null {
  if (!reason) return null
  return WEB_WORKSPACE_DENIAL_REASON_KEYS[reason] ?? null
}

/** Message shown when the capability probe itself fails. */
export function resolveAccessErrorMessage(
  error: WebWorkspaceErrorInfo | null
): string {
  return error?.messageKey ?? 'Something went wrong. Please try again.'
}
