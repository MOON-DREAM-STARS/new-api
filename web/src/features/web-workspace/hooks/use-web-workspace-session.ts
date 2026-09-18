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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import {
  fetchWebWorkspaceSession,
  restartWebWorkspaceSession,
  startWebWorkspaceSession,
  stopWebWorkspaceSession,
} from '../api'
import {
  WEB_WORKSPACE_SESSION_POLL_INTERVAL_MS,
  WEB_WORKSPACE_SESSION_QUERY_KEY,
} from '../constants'
import { useDocumentVisibility } from './use-document-visibility'

export type UseWebWorkspaceSessionOptions = {
  /** False while the account has no access, so no request is issued. */
  enabled?: boolean
}

/**
 * Current session state. Polling runs only while the page is visible, and the
 * cached value stays on screen when the tab is hidden.
 */
export function useWebWorkspaceSession(
  options: UseWebWorkspaceSessionOptions = {}
) {
  const isVisible = useDocumentVisibility()
  const isEnabled = options.enabled ?? true
  return useQuery({
    queryKey: WEB_WORKSPACE_SESSION_QUERY_KEY,
    queryFn: fetchWebWorkspaceSession,
    enabled: isEnabled && isVisible,
    refetchInterval:
      isEnabled && isVisible ? WEB_WORKSPACE_SESSION_POLL_INTERVAL_MS : false,
    retry: false,
  })
}

function useSyncSessionCache() {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({ queryKey: WEB_WORKSPACE_SESSION_QUERY_KEY })
  }
}

export function useStartWebWorkspaceSession() {
  const syncSessionCache = useSyncSessionCache()
  return useMutation({
    mutationFn: startWebWorkspaceSession,
    onSuccess: syncSessionCache,
  })
}

export function useStopWebWorkspaceSession() {
  const syncSessionCache = useSyncSessionCache()
  return useMutation({
    mutationFn: stopWebWorkspaceSession,
    onSuccess: syncSessionCache,
  })
}

export function useRestartWebWorkspaceSession() {
  const syncSessionCache = useSyncSessionCache()
  return useMutation({
    mutationFn: restartWebWorkspaceSession,
    onSuccess: syncSessionCache,
  })
}
