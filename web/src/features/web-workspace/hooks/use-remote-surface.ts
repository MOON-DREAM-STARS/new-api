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
import RFB from '@novnc/novnc'
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'

import { createWebWorkspaceStreamTicket } from '../api'
import { WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS } from '../constants'
import { classifyWebWorkspaceError } from '../lib/errors'
import { canRetryReconnect, reconnectDelayMs } from '../lib/reconnect'
import { buildStreamWebSocketUrl } from '../lib/stream-url'

export type RemoteSurfaceStatus =
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'failed'

export type RemoteSurfaceController = {
  /** Host element noVNC renders its canvas into. */
  containerRef: RefObject<HTMLDivElement | null>
  /** Remote framebuffer size in remote pixels once the stream is up. */
  screen: { width: number; height: number } | null
  status: RemoteSurfaceStatus
  /** Failed automatic attempts. Reset on a successful connect. */
  attempt: number
  maxAttempts: number
  errorMessageKey: string | null
  reconnect: () => void
}

export type UseRemoteSurfaceOptions = {
  sessionId: string
  /** False while the session is not live or the surface is unsupported. */
  enabled: boolean
}

/**
 * Streams the remote browser into `containerRef` through noVNC.
 *
 * Ticket flow per attach: `POST /session/:id/stream-ticket` → same-origin
 * `ws/wss` URL derived from the page location → the open socket is handed to
 * noVNC, which owns the RFB stream, keyboard and mouse input. A closed socket
 * triggers a fresh ticket with capped exponential backoff; after the attempt
 * limit the controller stops retrying and waits for a manual reconnect.
 */
export function useRemoteSurface(
  options: UseRemoteSurfaceOptions
): RemoteSurfaceController {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const rfbRef = useRef<RFB | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const timerRef = useRef<number | null>(null)
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
    if (timerRef.current === null) return
    window.clearTimeout(timerRef.current)
    timerRef.current = null
  }, [])

  const teardown = useCallback(() => {
    clearTimer()
    const rfb = rfbRef.current
    rfbRef.current = null
    if (rfb) {
      // A stale RFB instance must not drive reconnect decisions.
      rfb.disconnect()
    }
    const socket = socketRef.current
    socketRef.current = null
    if (socket && socket.readyState !== WebSocket.CLOSED) {
      socket.close()
    }
  }, [clearTimer])

  const sessionId = options.sessionId
  const enabled = options.enabled

  useEffect(() => {
    if (!enabled || !sessionId) {
      generationRef.current += 1
      teardown()
      setStatus('connecting')
      setAttempt(0)
      return undefined
    }

    const generation = generationRef.current + 1
    generationRef.current = generation
    attemptsRef.current = 0
    setAttempt(0)
    setStatus('connecting')
    setErrorMessageKey(null)

    function scheduleReconnect() {
      if (generationRef.current !== generation) return
      const nextAttempt = attemptsRef.current + 1
      attemptsRef.current = nextAttempt
      setAttempt(nextAttempt)
      if (!canRetryReconnect(nextAttempt - 1)) {
        setStatus('failed')
        return
      }
      setStatus('reconnecting')
      timerRef.current = window.setTimeout(() => {
        timerRef.current = null
        connect()
      }, reconnectDelayMs(nextAttempt))
    }

    async function attach() {
      const container = containerRef.current
      if (!container) return
      try {
        const ticket = await createWebWorkspaceStreamTicket(sessionId)
        if (generationRef.current !== generation) return
        const url = buildStreamWebSocketUrl(ticket.stream_url, ticket.ticket, {
          protocol: window.location.protocol,
          host: window.location.host,
        })
        const socket = new WebSocket(url)
        if (generationRef.current !== generation) {
          socket.close()
          return
        }
        socketRef.current = socket
        // noVNC 1.7 ignores the presentation options passed to the
        // constructor, so the viewport scaling has to be applied on the
        // instance. Scaling maps the real framebuffer onto the presentation
        // stage; the remote desktop keeps its own size because a remote resize
        // would move the framebuffer under the presentation crop.
        const rfb = new RFB(container, socket)
        rfb.scaleViewport = true
        // Pin the low-bandwidth text profile explicitly so a noVNC default
        // change does not silently raise stream bytes. The package's public
        // types lag its runtime API, so the two supported setters are typed
        // locally instead of weakening the RFB instance type.
        const bandwidthProfile = rfb as unknown as {
          compressionLevel: number
          qualityLevel: number
        }
        bandwidthProfile.compressionLevel = 2
        bandwidthProfile.qualityLevel = 6
        rfbRef.current = rfb
        rfb.addEventListener('connect', () => {
          if (generationRef.current !== generation) return
          attemptsRef.current = 0
          setAttempt(0)
          setStatus('connected')
          setErrorMessageKey(null)
        })
        rfb.addEventListener('disconnect', () => {
          if (generationRef.current !== generation) return
          if (rfbRef.current !== rfb) return
          rfbRef.current = null
          socketRef.current = null
          scheduleReconnect()
        })
        rfb.addEventListener('securityfailure', () => {
          if (generationRef.current !== generation) return
          setErrorMessageKey('Could not verify the remote browser session.')
        })
      } catch (error) {
        if (generationRef.current !== generation) return
        setErrorMessageKey(classifyWebWorkspaceError(error).messageKey)
        scheduleReconnect()
      }
    }

    function connect() {
      if (generationRef.current !== generation) return
      if (!containerRef.current) return
      clearTimer()
      setErrorMessageKey(null)
      const previousRfb = rfbRef.current
      rfbRef.current = null
      if (previousRfb) {
        previousRfb.disconnect()
      }
      void attach()
    }

    connect()

    return () => {
      generationRef.current += 1
      teardown()
    }
  }, [enabled, sessionId, reconnectToken, clearTimer, teardown])

  useEffect(() => {
    if (status !== 'connected') {
      setScreen(null)
      return
    }
    const canvas = containerRef.current?.querySelector('canvas')
    if (!canvas) return
    setScreen({ width: canvas.width, height: canvas.height })
  }, [status])

  const reconnect = useCallback(() => {
    attemptsRef.current = 0
    setReconnectToken((token) => token + 1)
  }, [])

  return {
    containerRef,
    screen,
    status,
    attempt,
    maxAttempts: WEB_WORKSPACE_RECONNECT_MAX_ATTEMPTS,
    errorMessageKey,
    reconnect,
  }
}
