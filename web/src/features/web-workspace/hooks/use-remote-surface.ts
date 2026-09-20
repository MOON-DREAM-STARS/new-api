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
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'

import { createWebWorkspaceStreamTicket } from '../api'
import { WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS } from '../constants'
import { classifyWebWorkspaceError } from '../lib/errors'
import type { LocalCanvasMetrics } from '../lib/local-input'
import { canRetryReconnect, reconnectDelayMs } from '../lib/reconnect'

export type RemoteSurfaceStatus =
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'failed'

export type RemoteSurfaceController = {
  /** Host element into which the KasmVNC iframe is mounted. */
  containerRef: RefObject<HTMLDivElement | null>
  /** Remote framebuffer size in remote pixels once the stream is up. */
  screen: { width: number; height: number } | null
  status: RemoteSurfaceStatus
  /** Failed automatic attempts. Reset on a successful connect. */
  attempt: number
  maxAttempts: number
  errorMessageKey: string | null
  getIframe: () => HTMLIFrameElement | null
  getCanvasMetrics: () => LocalCanvasMetrics | null
  focusSurface: () => void
  reconnect: () => void
}

export type UseRemoteSurfaceOptions = {
  sessionId: string
  /** False while the session is not live or the surface is unsupported. */
  enabled: boolean
}

const KASM_SURFACE_ERROR_KEY = 'Could not connect to the remote browser.'
const KASM_SCREEN_PROBE_INTERVAL_MS = 250
const KASM_CONNECT_TIMEOUT_MS = 30000

/** @internal Exported for the KasmVNC URL contract test. */
export function buildKasmClientUrl(sessionId: string, ticket: string): string {
  const sessionPath = encodeURIComponent(sessionId)
  const ticketPath = encodeURIComponent(ticket)
  const base = `/api/web-workspace/session/${sessionPath}/kasm/t/${ticketPath}/vnc.html`
  const params = new URLSearchParams({
    autoconnect: '1',
    resize: 'scale',
    // KasmVNC 1.5.0 prefixes the websocket URL with "/", so the path has to
    // include the complete same-origin API prefix instead of a bare
    // "websockify" segment. The ticket remains in the path so KasmVNC's
    // relative assets and websocket request stay authorized.
    path: `api/web-workspace/session/${sessionPath}/kasm/t/${ticketPath}/websockify`,
    // KasmVNC defaults these to enabled when embedded. Force them off so the
    // authenticated X11 clipd bridge is the only clipboard path.
    clipboard_up: '0',
    clipboard_down: '0',
    clipboard_seamless: '0',
  })
  return `${base}?${params.toString()}`
}

/**
 * Renders the KasmVNC web client in a same-origin iframe and keeps the local
 * workspace status/error surface in sync with the client's postMessage state.
 *
 * The iframe's relative assets and the client's websocket request both stay
 * under the authenticated `/kasm/` API prefix. KasmVNC's own IME path is
 * disabled; the workspace's local-input anchor owns composition on the host
 * and injects only the committed text.
 */
export function useRemoteSurface(
  options: UseRemoteSurfaceOptions
): RemoteSurfaceController {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  // The ticket whose client document is currently loaded in the iframe. A
  // second ticket refreshes that document in place; it never replaces the
  // mounted element, so the Kasm surface is mounted exactly once per session and
  // its websocket and remote framebuffer are not torn down underneath the
  // operator.
  const mountedSrcRef = useRef<string | null>(null)
  // Identity of the session the mounted surface belongs to. A reconnect re-runs
  // the attach effect but must keep the mounted element; only a real session,
  // availability or visibility transition may remove it.
  const mountedSessionKeyRef = useRef<string | null>(null)
  const timerRef = useRef<number | null>(null)
  const startupTimerRef = useRef<number | null>(null)
  const screenProbeTimerRef = useRef<number | null>(null)
  const attemptsRef = useRef(0)
  const generationRef = useRef(0)
  const [reconnectToken, setReconnectToken] = useState(0)
  const [status, setStatus] = useState<RemoteSurfaceStatus>('connecting')
  const [attempt, setAttempt] = useState(0)
  const [errorMessageKey, setErrorMessageKey] = useState<string | null>(null)
  const [screen, setScreen] = useState<{
    width: number
    height: number
  } | null>(null)

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current)
      timerRef.current = null
    }
    if (startupTimerRef.current !== null) {
      window.clearTimeout(startupTimerRef.current)
      startupTimerRef.current = null
    }
    if (screenProbeTimerRef.current !== null) {
      window.clearTimeout(screenProbeTimerRef.current)
      screenProbeTimerRef.current = null
    }
  }, [])

  const teardown = useCallback(() => {
    clearTimer()
    const iframe = iframeRef.current
    iframeRef.current = null
    canvasRef.current = null
    mountedSrcRef.current = null
    mountedSessionKeyRef.current = null
    if (iframe) iframe.remove()
  }, [clearTimer])

  const sessionId = options.sessionId
  const enabled = options.enabled

  useEffect(() => {
    if (!enabled || !sessionId) {
      generationRef.current += 1
      teardown()
      setStatus('connecting')
      setAttempt(0)
      setScreen(null)
      return undefined
    }

    const container = containerRef.current
    if (!container) return undefined

    // A reconnect re-runs this effect with the same session key. The mounted
    // surface must survive that run: only a genuine session/availability change
    // is allowed to remove the element and drop its websocket.
    const sessionKey = `${sessionId}`
    const isRemount = mountedSessionKeyRef.current !== sessionKey
    mountedSessionKeyRef.current = sessionKey

    const generation = generationRef.current + 1
    generationRef.current = generation
    attemptsRef.current = 0
    setAttempt(0)
    setStatus('connecting')
    setErrorMessageKey(null)
    if (isRemount) setScreen(null)

    function scheduleReconnect() {
      if (generationRef.current !== generation) return
      clearTimer()
      const nextAttempt = attemptsRef.current + 1
      attemptsRef.current = nextAttempt
      setAttempt(nextAttempt)
      if (!canRetryReconnect(nextAttempt - 1)) {
        setStatus('failed')
        setErrorMessageKey(KASM_SURFACE_ERROR_KEY)
        return
      }
      setStatus('reconnecting')
      timerRef.current = window.setTimeout(() => {
        timerRef.current = null
        setReconnectToken((token) => token + 1)
      }, reconnectDelayMs(nextAttempt))
    }

    let iframe: HTMLIFrameElement | null = null

    function probeScreen() {
      if (generationRef.current !== generation || !iframe) return
      const canvases = Array.from(
        iframe.contentDocument?.querySelectorAll('canvas') ?? []
      )
      // KasmVNC's page contains a small multi-monitor widget canvas before the
      // real remote framebuffer canvas. Select the largest usable canvas so the
      // presentation crop never derives from the widget.
      const canvas = canvases
        .filter(
          (candidate) => candidate.width >= 640 && candidate.height >= 360
        )
        .sort(
          (left, right) => right.width * right.height - left.width * left.height
        )[0]
      if (canvas && canvas.width > 0 && canvas.height > 0) {
        canvasRef.current = canvas
        setScreen({ width: canvas.width, height: canvas.height })
        return
      }
      canvasRef.current = null
      screenProbeTimerRef.current = window.setTimeout(
        probeScreen,
        KASM_SCREEN_PROBE_INTERVAL_MS
      )
    }

    function markConnected() {
      if (generationRef.current !== generation) return
      clearTimer()
      attemptsRef.current = 0
      setAttempt(0)
      setStatus('connected')
      setErrorMessageKey(null)
      probeScreen()
    }

    function handleMessage(event: MessageEvent) {
      if (generationRef.current !== generation || !iframe) return
      if (event.source !== iframe.contentWindow) return
      if (event.origin !== window.location.origin) return
      const data = event.data as { action?: string; value?: unknown } | null
      if (!data || typeof data.action !== 'string') return

      switch (data.action) {
        case 'noVNC_initialized':
          break
        case 'connection_state':
          if (data.value === 'connected') {
            markConnected()
          } else if (data.value === 'disconnected') {
            scheduleReconnect()
          } else if (data.value === 'reconnecting') {
            setStatus('reconnecting')
          } else if (data.value === 'connecting') {
            setStatus('connecting')
          }
          break
        case 'disconnectrx':
        case 'idle_session_timeout':
          setErrorMessageKey(KASM_SURFACE_ERROR_KEY)
          scheduleReconnect()
          break
        default:
          break
      }
    }

    // attachSurface mounts the KasmVNC client exactly once per session. A later
    // ticket (for example after a reconnect) reuses the same iframe element and
    // only navigates it to the new authorised URL, so the element, its
    // websocket and the remote framebuffer are never recreated underneath the
    // operator and the replacement surface never doubles up in the DOM.
    function attachSurface(ticket: string) {
      if (generationRef.current !== generation || !container) return
      const nextSrc = buildKasmClientUrl(sessionId, ticket)
      const current = iframeRef.current
      if (current && current.isConnected) {
        mountedSrcRef.current = nextSrc
        if (current.getAttribute('src') !== nextSrc) current.src = nextSrc
        return
      }
      const nextIframe = document.createElement('iframe')
      iframe = nextIframe
      iframeRef.current = nextIframe
      mountedSrcRef.current = nextSrc
      nextIframe.title = 'Remote browser display'
      nextIframe.dataset.testid = 'web-workspace-surface-iframe'
      nextIframe.setAttribute(
        'allow',
        'clipboard-read; clipboard-write; fullscreen; autoplay'
      )
      nextIframe.setAttribute('allowfullscreen', 'true')
      nextIframe.style.width = '100%'
      nextIframe.style.height = '100%'
      nextIframe.style.display = 'block'
      nextIframe.style.border = '0'
      nextIframe.style.background = '#000'
      nextIframe.addEventListener('load', () => {
        if (generationRef.current !== generation) return
      })
      nextIframe.addEventListener('error', () => {
        if (generationRef.current !== generation) return
        scheduleReconnect()
      })
      nextIframe.src = nextSrc
      window.addEventListener('message', handleMessage)
      container.replaceChildren(nextIframe)
      startupTimerRef.current = window.setTimeout(() => {
        startupTimerRef.current = null
        if (generationRef.current !== generation) return
        scheduleReconnect()
      }, KASM_CONNECT_TIMEOUT_MS)
    }

    void createWebWorkspaceStreamTicket(sessionId)
      .then(({ ticket }) => {
        if (generationRef.current !== generation) return
        attachSurface(ticket)
      })
      .catch((error) => {
        if (generationRef.current !== generation) return
        setErrorMessageKey(classifyWebWorkspaceError(error).messageKey)
        scheduleReconnect()
      })

    return () => {
      window.removeEventListener('message', handleMessage)
      if (generationRef.current === generation) {
        generationRef.current += 1
      }
      // Keep the mounted Kasm surface across a reconnect of the same session so
      // the websocket and framebuffer are never rebuilt underneath the operator.
      if (mountedSessionKeyRef.current !== sessionKey) teardown()
    }
  }, [enabled, sessionId, reconnectToken, teardown])

  const reconnect = useCallback(() => {
    attemptsRef.current = 0
    setReconnectToken((token) => token + 1)
  }, [])

  const getIframe = useCallback(() => iframeRef.current, [])

  const getCanvasMetrics = useCallback((): LocalCanvasMetrics | null => {
    const container = containerRef.current
    const iframe = iframeRef.current
    const canvas = canvasRef.current
    if (!container || !iframe || !canvas || canvas.width <= 0 || canvas.height <= 0) {
      return null
    }
    const containerRect = container.getBoundingClientRect()
    const iframeRect = iframe.getBoundingClientRect()
    const canvasRect = canvas.getBoundingClientRect()
    return {
      left: iframeRect.left - containerRect.left + canvasRect.left,
      top: iframeRect.top - containerRect.top + canvasRect.top,
      width: canvasRect.width,
      height: canvasRect.height,
      canvasWidth: canvas.width,
      canvasHeight: canvas.height,
    }
  }, [])

  const focusSurface = useCallback(() => {
    const iframe = iframeRef.current
    if (!iframe) return
    iframe.focus()
    iframe.contentWindow?.focus()
    containerRef.current?.focus()
  }, [])

  return {
    containerRef,
    screen,
    status,
    attempt,
    maxAttempts: WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS,
    errorMessageKey,
    getIframe,
    getCanvasMetrics,
    focusSurface,
    reconnect,
  }
}
