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
import { WEB_WORKSPACE_STATUS_QUERY_KEY } from '../constants'
import { readWebWorkspaceDenialReason } from '../lib/errors'

async function fetchDenialReason(): Promise<string | null> {
  try {
    const status = await fetchWebWorkspaceStatus()
    return status.entitled ? null : (status.reason ?? null)
  } catch (error) {
    // A denial is the expected answer for this probe.
    return readWebWorkspaceDenialReason(error)
  }
}

/**
 * Reads the entitlement denial reason so the disabled page can explain why the
 * account has no access. A denial is an expected answer, never an error toast.
 */
export function useWebWorkspaceDenialReason(enabled: boolean): string | null {
  const { data } = useQuery({
    queryKey: WEB_WORKSPACE_STATUS_QUERY_KEY,
    queryFn: fetchDenialReason,
    enabled,
    retry: false,
    staleTime: 60 * 1000,
  })
  return data ?? null
}
