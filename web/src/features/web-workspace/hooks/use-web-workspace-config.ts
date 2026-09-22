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
import { queryOptions, useQuery } from '@tanstack/react-query'

import { useAuthStore } from '@/stores/auth-store'

import { fetchWebWorkspaceConfig } from '../api'
import {
  WEB_WORKSPACE_CONFIG_QUERY_KEY,
  WEB_WORKSPACE_CONFIG_STALE_TIME_MS,
} from '../constants'

/**
 * Shared capability probe. The route guard primes this cache before the page
 * renders, so the page and the sidebar entry read the same answer.
 */
export const webWorkspaceConfigQueryOptions = queryOptions({
  queryKey: WEB_WORKSPACE_CONFIG_QUERY_KEY,
  queryFn: fetchWebWorkspaceConfig,
  staleTime: WEB_WORKSPACE_CONFIG_STALE_TIME_MS,
  retry: false,
})

export function useWebWorkspaceConfig() {
  return useQuery(webWorkspaceConfigQueryOptions)
}

/**
 * Whether the sidebar entry may be rendered. Hiding the entry is UX only —
 * the route guard and every API call enforce entitlement independently.
 */
export function useWebWorkspaceEntryVisible(): boolean {
  const hasUser = useAuthStore((state) => Boolean(state.auth.user))
  const { data } = useQuery({
    ...webWorkspaceConfigQueryOptions,
    enabled: hasUser,
  })
  return Boolean(data?.enabled && data?.entitled)
}
