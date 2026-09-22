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
import { useQuery } from '@tanstack/react-query'

import { fetchWebWorkspaceStatus } from '../api'

export const WEB_WORKSPACE_PROVIDER_QUERY_KEY = [
  'web-workspace',
  'provider',
] as const

async function fetchProvider(): Promise<string | null> {
  const status = await fetchWebWorkspaceStatus()
  return status.workspace?.provider ?? null
}

/**
 * Reads the provider of the caller workspace. The presentation adapter needs
 * it to pick the calibrated crop profile; an unknown provider means the
 * workspace view fails closed instead of showing the provider shell.
 */
export function useWebWorkspaceProvider(enabled: boolean): string | null {
  const { data } = useQuery({
    queryKey: WEB_WORKSPACE_PROVIDER_QUERY_KEY,
    queryFn: fetchProvider,
    enabled,
    retry: false,
    staleTime: 5 * 60 * 1000,
  })
  return data ?? null
}
