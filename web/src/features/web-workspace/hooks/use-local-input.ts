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
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type CompositionEvent,
  type FormEvent,
  type KeyboardEvent,
  type RefObject,
} from 'react'

import {
  dispatchWebWorkspaceInputKey,
  getWebWorkspaceInputCaret,
  insertWebWorkspaceInputText,
} from '../api'
import { classifyWebWorkspaceError } from '../lib/errors'
import {
  centerOfCanvas,
  mapCaretToAnchor,
  resolveLocalInputKey,
} from '../lib/local-input'
import type {
  WebWorkspaceInputModifier,
  WebWorkspaceInputMode,
} from '../types'
import type { RemoteSurfaceController } from './use-remote-surface'

export type UseLocalInputOptions = {
  sessionId: string | null
  enabled: boolean
  surface: RemoteSurfaceController
  /** Returns true when the clipboard bridge handled the shortcut. */
  copyRemoteSelection?: () => Promise<boolean>
  /** Returns true when the clipboard bridge handled the shortcut. */
  pasteIntoRemote?: () => Promise<boolean>
}

export type LocalInputController = {
  mode: WebWorkspaceInputMode
  enabled: boolean
  anchorRef: RefObject<HTMLInputElement | null>
  anchorStyle: CSSProperties
  errorCode: string | null
  toggle: () => void
  handleSurfaceClick: () => void
  handleCompositionStart: () => void
  handleCompositionEnd: (event: CompositionEvent<HTMLInputElement>) => void
  handleInput: (event: FormEvent<HTMLInputElement>) => void
  handleKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void
}

const CENTER_STYLE: CSSProperties = { left: '50%', top: '50%' }
const CARET_REFRESH_DEBOUNCE_MS = 150

export function useLocalInput(
  options: UseLocalInputOptions
): LocalInputController {
  const { enabled, sessionId, surface } = options
  const anchorRef = useRef<HTMLInputElement | null>(null)
  const modeRef = useRef<WebWorkspaceInputMode>('remote')
  const compositionActiveRef = useRef(false)
  const skipNextInputRef = useRef(false)
  const injectionChainRef = useRef<Promise<void>>(Promise.resolve())
  const caretRefreshTimerRef = useRef<number | null>(null)
  const caretProbeIdRef = useRef(0)
  const [mode, setModeState] = useState<WebWorkspaceInputMode>('remote')
  const [anchorStyle, setAnchorStyle] = useState<CSSProperties>(CENTER_STYLE)
  const [errorCode, setErrorCode] = useState<string | null>(null)

  const reportError = useCallback((error: unknown) => {
    const info = classifyWebWorkspaceError(error)
    setErrorCode(info.code ?? 'WEB_WORKSPACE_INPUT_UNAVAILABLE')
  }, [])

  const refreshCaret = useCallback(async (reportFailure = true) => {
    if (!enabled || !sessionId) return
    const probeId = ++caretProbeIdRef.current
    try {
      const caret = await getWebWorkspaceInputCaret(sessionId)
      if (probeId !== caretProbeIdRef.current) return
      const canvas = surface.getCanvasMetrics()
      const mapped = mapCaretToAnchor(caret, canvas) ?? centerOfCanvas(canvas)
      setAnchorStyle(mapped ? { left: mapped.left, top: mapped.top } : CENTER_STYLE)
    } catch (error) {
      if (reportFailure && probeId === caretProbeIdRef.current) reportError(error)
    }
  }, [enabled, reportError, sessionId, surface])

  const clearCaretRefresh = useCallback(() => {
    // A newer injection owns the caret position; ignore any probe that was
    // already in flight so its failure cannot overwrite the newer success.
    caretProbeIdRef.current += 1
    if (caretRefreshTimerRef.current !== null) {
      window.clearTimeout(caretRefreshTimerRef.current)
      caretRefreshTimerRef.current = null
    }
  }, [])

  const scheduleCaretRefresh = useCallback(() => {
    clearCaretRefresh()
    caretRefreshTimerRef.current = window.setTimeout(() => {
      caretRefreshTimerRef.current = null
      void refreshCaret(false)
    }, CARET_REFRESH_DEBOUNCE_MS)
  }, [clearCaretRefresh, refreshCaret])

  const setMode = useCallback(
    (next: WebWorkspaceInputMode) => {
      if (!enabled && next === 'local') return
      modeRef.current = next
      setModeState(next)
      setErrorCode(null)
      if (next === 'local') {
        void refreshCaret()
        window.requestAnimationFrame(() => anchorRef.current?.focus())
      } else {
        clearCaretRefresh()
        surface.focusSurface()
      }
    },
    [clearCaretRefresh, enabled, refreshCaret, surface]
  )

  const injectText = useCallback(
    (text: string) => {
      if (!enabled || !sessionId || !text) return
      const run = async () => {
        clearCaretRefresh()
        try {
          await insertWebWorkspaceInputText(sessionId, text)
        } catch (error) {
          // Keep the local composition intact on failure so the operator can
          // retry without silently replaying raw keys into the remote browser.
          if (anchorRef.current) anchorRef.current.value = text
          reportError(error)
          anchorRef.current?.focus()
          return
        }
        setErrorCode(null)
        if (anchorRef.current?.value === text) {
          anchorRef.current.value = ''
        }
        scheduleCaretRefresh()
      }
      const next = injectionChainRef.current.catch(() => undefined).then(run)
      injectionChainRef.current = next.catch(() => undefined)
    },
    [
      clearCaretRefresh,
      enabled,
      reportError,
      scheduleCaretRefresh,
      sessionId,
    ]
  )

  const dispatchKey = useCallback(
    (key: string, modifiers: WebWorkspaceInputModifier[]) => {
      if (!enabled || !sessionId) return
      void dispatchWebWorkspaceInputKey(sessionId, key, modifiers)
        .then(() => setErrorCode(null))
        .catch(reportError)
    },
    [enabled, reportError, sessionId]
  )

  const toggle = useCallback(() => {
    setMode(modeRef.current === 'local' ? 'remote' : 'local')
  }, [setMode])

  const handleSurfaceClick = useCallback(() => {
    if (modeRef.current !== 'local') return
    anchorRef.current?.focus()
    void refreshCaret()
  }, [refreshCaret])

  const handleCompositionStart = useCallback(() => {
    compositionActiveRef.current = true
  }, [])

  const handleCompositionEnd = useCallback(
    (event: CompositionEvent<HTMLInputElement>) => {
      compositionActiveRef.current = false
      const text = event.data || event.currentTarget.value
      skipNextInputRef.current = true
      if (text) injectText(text)
    },
    [injectText]
  )

  const handleInput = useCallback(
    (event: FormEvent<HTMLInputElement>) => {
      if (compositionActiveRef.current) return
      if (skipNextInputRef.current) {
        skipNextInputRef.current = false
        return
      }
      const text = event.currentTarget.value
      if (text) injectText(text)
    },
    [injectText]
  )

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (modeRef.current !== 'local' || event.nativeEvent.isComposing) return
      const resolved = resolveLocalInputKey(event.nativeEvent)
      if (!resolved) return
      event.preventDefault()

      if (resolved.key === 'Escape') {
        dispatchKey(resolved.key, resolved.modifiers)
        setMode('remote')
        return
      }

      if (resolved.key === 'c' || resolved.key === 'v') {
        const bridge = resolved.key === 'c' ? options.copyRemoteSelection : options.pasteIntoRemote
        void (async () => {
          try {
            if (bridge && (await bridge())) return
          } catch {
            // Fall through to a real remote key event when the clipboard bridge
            // is unavailable or denied.
          }
          dispatchKey(resolved.key, resolved.modifiers)
        })()
        return
      }

      dispatchKey(resolved.key, resolved.modifiers)
    },
    [
      dispatchKey,
      options.copyRemoteSelection,
      options.pasteIntoRemote,
      setMode,
    ]
  )

  useEffect(() => {
    if (enabled) return undefined
    clearCaretRefresh()
    modeRef.current = 'remote'
    setModeState('remote')
    setErrorCode(null)
    compositionActiveRef.current = false
    skipNextInputRef.current = false
    return undefined
  }, [clearCaretRefresh, enabled])

  useEffect(() => clearCaretRefresh, [clearCaretRefresh])

  // While local input owns typing, a click inside the remote page must hand
  // focus back to the anchor: the iframe would otherwise keep the keyboard and
  // the local IME would never see the next keystroke. The listener follows the
  // iframe document because a reconnect reloads it with a fresh ticket.
  useEffect(() => {
    if (!enabled || mode !== 'local') return undefined
    const frame = surface.getIframe()
    if (!frame) return undefined
    const handleClick = () => handleSurfaceClick()
    let attached: Document | null = null
    const attach = () => {
      const next = frame.contentDocument
      if (!next || next === attached) return
      attached?.removeEventListener('click', handleClick, true)
      attached = next
      attached.addEventListener('click', handleClick, true)
    }
    attach()
    frame.addEventListener('load', attach)
    return () => {
      frame.removeEventListener('load', attach)
      attached?.removeEventListener('click', handleClick, true)
    }
  }, [enabled, handleSurfaceClick, mode, surface])

  return {
    mode,
    enabled,
    anchorRef,
    anchorStyle,
    errorCode,
    toggle,
    handleSurfaceClick,
    handleCompositionStart,
    handleCompositionEnd,
    handleInput,
    handleKeyDown,
  }
}
