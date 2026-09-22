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
  WebWorkspaceFileChooser,
  WebWorkspaceFileChooserResult,
  WebWorkspaceInputCaret,
  WebWorkspaceInputCaretResponse,
  WebWorkspaceInputModifier,
  WebWorkspaceNavigationAction,
  WebWorkspaceSession,
  WebWorkspaceStatus,
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
  action: WebWorkspaceNavigationAction,
  projectId?: number
): Promise<WebWorkspaceSession> {
  const res = await api.post<ApiEnvelope<WebWorkspaceSession>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/navigation`,
    projectId === undefined ? { action } : { action, project_id: projectId }
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
export async function createWebWorkspaceProjectPermit(
  name: string
): Promise<WebProjectPermit> {
  const res = await api.post<ApiEnvelope<WebProjectPermit>>(
    `${BASE_PATH}/projects`,
    { name }
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

/**
 * Long-polls the control plane for a pending chooser. A timeout returns null so
 * the caller can immediately start the next poll without treating it as error.
 */
export async function fetchWebWorkspaceFileChooser(
  sessionId: string,
  waitMs = 25000,
  signal?: AbortSignal
): Promise<WebWorkspaceFileChooser | null> {
  const res = await api.get<ApiEnvelope<WebWorkspaceFileChooser | null>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/file-chooser`,
    {
      params: { wait_ms: waitMs },
      signal,
      skipErrorHandler: true,
      disableDuplicate: true,
    }
  )
  return res.data.data ?? null
}

/** Streams selected local files to the agent through the control plane. */
export async function uploadWebWorkspaceFileChooserFiles(
  sessionId: string,
  chooserId: string,
  formData: FormData
): Promise<WebWorkspaceFileChooserResult> {
  const res = await api.post<ApiEnvelope<WebWorkspaceFileChooserResult>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/file-chooser/${encodeURIComponent(chooserId)}/files`,
    formData,
    { skipErrorHandler: true }
  )
  return res.data.data
}

/** Cancels a pending chooser when the operator dismisses the local picker. */
export async function cancelWebWorkspaceFileChooser(
  sessionId: string,
  chooserId: string
): Promise<WebWorkspaceFileChooserResult> {
  const res = await api.post<ApiEnvelope<WebWorkspaceFileChooserResult>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/file-chooser/${encodeURIComponent(chooserId)}/cancel`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data.data
}

export type WebWorkspaceClipboardPayload = {
  mime: string
  data: Blob
}

/** Returns the raw remote selection bytes and MIME type. */
export async function copyWebWorkspaceClipboard(
  sessionId: string
): Promise<WebWorkspaceClipboardPayload> {
  const res = await api.post<Blob>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/clipboard/copy`,
    undefined,
    { responseType: 'blob', skipErrorHandler: true }
  )
  const rawContentType = String(res.headers['content-type'] ?? '')
  const mime = rawContentType.split(';', 1)[0]?.trim().toLowerCase() ?? ''
  if (!mime) throw new Error('clipboard response is missing Content-Type')
  return { mime, data: res.data }
}

/** Sends one raw local clipboard body to the remote browser. */
export async function pasteWebWorkspaceClipboard(
  sessionId: string,
  mime: string,
  data: Blob
): Promise<void> {
  await api.post(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/clipboard/paste`,
    data,
    {
      headers: { 'Content-Type': mime },
      skipErrorHandler: true,
    }
  )
}

/** Injects one committed local text value into the remote browser. */
export async function insertWebWorkspaceInputText(
  sessionId: string,
  text: string
): Promise<void> {
  await api.post(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/input/text`,
    { text },
    { skipErrorHandler: true }
  )
}

/** Forwards one approved remote key with modifiers. */
export async function dispatchWebWorkspaceInputKey(
  sessionId: string,
  key: string,
  modifiers: WebWorkspaceInputModifier[]
): Promise<void> {
  await api.post(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/input/key`,
    { key, modifiers },
    { skipErrorHandler: true }
  )
}

/** Reads the remote caret rectangle; null means no active editable element. */
export async function getWebWorkspaceInputCaret(
  sessionId: string
): Promise<WebWorkspaceInputCaret | null> {
  const res = await api.get<ApiEnvelope<WebWorkspaceInputCaretResponse>>(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/input/caret`,
    { skipErrorHandler: true }
  )
  return res.data.data?.caret ?? null
}

/** Issues one single-use ticket accepted by the KasmVNC iframe path. */
export async function createWebWorkspaceKasmTicket(
  sessionId: string
): Promise<string> {
  const res = await api.post<
    ApiEnvelope<{ ticket: string; expires_at: number }>
  >(
    `${BASE_PATH}/session/${encodeURIComponent(sessionId)}/kasm-ticket`
  )
  return res.data.data.ticket
}
