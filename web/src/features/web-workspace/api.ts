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
import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  WebProject,
  WebProjectPermit,
  WebWorkspaceConfig,
  WebWorkspaceNavigationAction,
  WebWorkspaceSession,
  WebWorkspaceStatus,
  WebWorkspaceStreamTicket,
} from './types'

const BASE_PATH = '/api/web-workspace'

export async function fetchWebWorkspaceConfig(): Promise<WebWorkspaceConfig> {
  const res = await api.get<ApiEnvelope<WebWorkspaceConfig>>(
    `${BASE_PATH}/config`
  )
  return res.data.data
}

/**
 * Entitlement probe used by the disabled state. A denial is an expected
 * answer here, so the global error toast is skipped.
 */
export async function fetchWebWorkspaceStatus(): Promise<WebWorkspaceStatus> {
  const res = await api.get<ApiEnvelope<WebWorkspaceStatus>>(
    `${BASE_PATH}/status`,
    { skipErrorHandler: true, disableDuplicate: true }
  )
  return res.data.data
}

export async function fetchWebWorkspaceSession(): Promise<WebWorkspaceSession | null> {
  const res = await api.get<ApiEnvelope<WebWorkspaceSession | null>>(
    `${BASE_PATH}/session`
  )
  return res.data.data ?? null
}

/** Remote screen size proposal. The agent validates the same range. */
export type WebWorkspaceScreenSize = {
  width: number
  height: number
}

/** Zero means "keep the agent default" for both dimensions. */
function screenSizePayload(screen?: WebWorkspaceScreenSize | null) {
  return {
    screen_width: screen?.width ?? 0,
    screen_height: screen?.height ?? 0,
  }
}

export async function startWebWorkspaceSession(
  screen?: WebWorkspaceScreenSize | null
): Promise<WebWorkspaceSession> {
  const res = await api.post<ApiEnvelope<WebWorkspaceSession>>(
    `${BASE_PATH}/session`,
    screenSizePayload(screen)
  )
  return res.data.data
}

export async function stopWebWorkspaceSession(
  sessionId: string
): Promise<void> {
  await api.delete(`${BASE_PATH}/session/${encodeURIComponent(sessionId)}`)
}

export type RestartWebWorkspaceSessionOptions = {
  /**
   * Only the "signed in, lock the runtime" transition is exposed here; opening
   * the sign-in window stays an operator action.
   */
  mode?: 'LOCKED'
  screenSize?: WebWorkspaceScreenSize | null
}

export async function restartWebWorkspaceSession(
  sessionId: string,
  options: RestartWebWorkspaceSessionOptions = {}
): Promise<WebWorkspaceSession> {
  const res = await api.post<ApiEnvelope<WebWorkspaceSession>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/restart`,
    { mode: options.mode ?? '', ...screenSizePayload(options.screenSize) }
  )
  return res.data.data
}

/**
 * Keeps the caller's live session alive without attaching a display stream. A
 * hidden tab calls this instead of holding the framebuffer stream open.
 */
export async function touchWebWorkspaceSession(
  sessionId: string
): Promise<WebWorkspaceSession> {
  const res = await api.post<ApiEnvelope<WebWorkspaceSession>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/activity`
  )
  return res.data.data
}
export async function navigateWebWorkspaceSession(
  sessionId: string,
  action: WebWorkspaceNavigationAction
): Promise<WebWorkspaceSession> {
  const res = await api.post<ApiEnvelope<WebWorkspaceSession>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/navigation`,
    { action }
  )
  return res.data.data
}

/**
 * Listing synchronises the guard observations first, so a refresh always shows
 * the server-side truth instead of a stale local cache.
 */
export async function fetchWebWorkspaceProjects(): Promise<WebProject[]> {
  const res = await api.get<ApiEnvelope<{ items: WebProject[] }>>(
    `${BASE_PATH}/projects`
  )
  return res.data.data?.items ?? []
}

/**
 * Issues a permit only. The project row is created later from the guard's
 * `project_created` observation.
 */
export async function createWebWorkspaceProjectPermit(): Promise<WebProjectPermit> {
  const res = await api.post<ApiEnvelope<WebProjectPermit>>(
    `${BASE_PATH}/projects`
  )
  return res.data.data
}

export async function renameWebWorkspaceProject(
  projectId: number,
  name: string
): Promise<WebProject> {
  const res = await api.patch<ApiEnvelope<WebProject>>(
    `${BASE_PATH}/projects/${projectId}`,
    { name }
  )
  return res.data.data
}

export async function deleteWebWorkspaceProject(
  projectId: number
): Promise<void> {
  await api.delete(`${BASE_PATH}/projects/${projectId}`)
}

export async function createWebWorkspaceStreamTicket(
  sessionId: string
): Promise<WebWorkspaceStreamTicket> {
  const res = await api.post<ApiEnvelope<WebWorkspaceStreamTicket>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/stream-ticket`
  )
  return res.data.data
}
