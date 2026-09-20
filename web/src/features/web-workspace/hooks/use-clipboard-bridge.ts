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
import { useCallback, useEffect, useState, type RefObject } from 'react'

import {
  copyWebWorkspaceClipboard,
  pasteWebWorkspaceClipboard,
  type WebWorkspaceClipboardPayload,
} from '../api'
import { classifyWebWorkspaceError } from '../lib/errors'

const CLIPBOARD_PERMISSION_ERROR_KEY =
  'Clipboard access was denied. Grant permission and try again.'
const CLIPBOARD_EMPTY_ERROR_KEY =
  'The clipboard does not contain supported text or image data.'
const CLIPBOARD_MIME_ORDER = [
  'text/plain',
  'image/png',
  'image/jpeg',
  'image/webp',
] as const

class ClipboardAccessError extends Error {}

export type UseClipboardBridgeOptions = {
  sessionId: string | null
  enabled: boolean
  containerRef: RefObject<HTMLElement | null>
}

export type ClipboardBridgeController = {
  errorMessageKey: string | null
  copyRemoteSelection: () => Promise<boolean>
  pasteIntoRemote: () => Promise<boolean>
}

function isEditableTarget(target: EventTarget | null): boolean {
  const element = target as { closest?: (selector: string) => Element | null } | null
  if (!element || typeof element.closest !== 'function') return false
  return Boolean(
    element.closest(
      'input, textarea, select, [contenteditable="true"], [role="textbox"], [role="dialog"]'
    )
  )
}

function isRemoteFocused(container: HTMLElement | null): boolean {
  const active = document.activeElement
  return Boolean(
    container && active && (active === container || container.contains(active))
  )
}

async function writeLocalClipboard(
  payload: WebWorkspaceClipboardPayload
): Promise<void> {
  try {
    if (!navigator.clipboard) throw new ClipboardAccessError()
    if (payload.mime === 'text/plain') {
      await navigator.clipboard.writeText(await payload.data.text())
      return
    }
    const ClipboardItemConstructor = window.ClipboardItem
    if (!ClipboardItemConstructor || !navigator.clipboard.write) {
      throw new ClipboardAccessError()
    }
    await navigator.clipboard.write([
      new ClipboardItemConstructor({ [payload.mime]: payload.data }),
    ])
  } catch (error) {
    if (error instanceof ClipboardAccessError) throw error
    throw new ClipboardAccessError()
  }
}

async function readLocalClipboard(): Promise<WebWorkspaceClipboardPayload | null> {
  const clipboard = navigator.clipboard
  if (!clipboard) throw new ClipboardAccessError()

  if (typeof clipboard.read === 'function') {
    let items: ClipboardItems
    try {
      items = await clipboard.read()
    } catch {
      throw new ClipboardAccessError()
    }
    for (const item of items) {
      for (const mime of CLIPBOARD_MIME_ORDER) {
        if (!item.types.includes(mime)) continue
        try {
          return { mime, data: await item.getType(mime) }
        } catch {
          throw new ClipboardAccessError()
        }
      }
    }
  }

  if (typeof clipboard.readText === 'function') {
    try {
      const text = await clipboard.readText()
      if (text) {
        return { mime: 'text/plain', data: new Blob([text], { type: 'text/plain' }) }
      }
    } catch {
      throw new ClipboardAccessError()
    }
  }
  return null
}

/**
 * Bridges the focused remote canvas to the local browser clipboard. The
 * Ctrl/Cmd shortcuts are only intercepted while the remote surface owns focus;
 * local inputs, dialogs and the rest of the page keep their native behavior.
 */
export function useClipboardBridge(
  options: UseClipboardBridgeOptions
): ClipboardBridgeController {
  const [errorMessageKey, setErrorMessageKey] = useState<string | null>(null)
  const { containerRef, enabled, sessionId } = options

  const reportError = useCallback((error: unknown) => {
    if (error instanceof ClipboardAccessError) {
      setErrorMessageKey(CLIPBOARD_PERMISSION_ERROR_KEY)
      return
    }
    setErrorMessageKey(classifyWebWorkspaceError(error).messageKey)
  }, [])

  const copyRemoteSelection = useCallback(async () => {
    if (!enabled || !sessionId) return false
    try {
      await writeLocalClipboard(await copyWebWorkspaceClipboard(sessionId))
      setErrorMessageKey(null)
      return true
    } catch (error) {
      reportError(error)
      return false
    }
  }, [enabled, reportError, sessionId])

  const pasteIntoRemote = useCallback(async () => {
    if (!enabled || !sessionId) return false
    try {
      const payload = await readLocalClipboard()
      if (!payload) {
        setErrorMessageKey(CLIPBOARD_EMPTY_ERROR_KEY)
        return false
      }
      await pasteWebWorkspaceClipboard(sessionId, payload.mime, payload.data)
      setErrorMessageKey(null)
      return true
    } catch (error) {
      reportError(error)
      return false
    }
  }, [enabled, reportError, sessionId])

  useEffect(() => {
    if (!enabled || !sessionId) return undefined

    const container = containerRef.current
    const documents = new Set<Document>()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        event.altKey ||
        event.shiftKey ||
        (!event.ctrlKey && !event.metaKey) ||
        !isRemoteFocused(containerRef.current) ||
        isEditableTarget(event.target)
      ) {
        return
      }
      const key = event.key.toLowerCase()
      if (key === 'c') {
        event.preventDefault()
        event.stopImmediatePropagation()
        void copyRemoteSelection()
      } else if (key === 'v') {
        event.preventDefault()
        event.stopImmediatePropagation()
        void pasteIntoRemote()
      }
    }
    const attachDocument = (doc: Document | null) => {
      if (!doc || documents.has(doc)) return
      documents.add(doc)
      doc.addEventListener('keydown', handleKeyDown, true)
    }
    const attachIframe = (frame: HTMLIFrameElement | null) => {
      if (!frame) return
      try {
        attachDocument(frame.contentDocument)
      } catch {
        // A cross-origin document cannot be bridged; keep the toolbar fallback.
      }
    }

    attachDocument(document)
    let iframe = container?.querySelector('iframe') as HTMLIFrameElement | null
    let onLoad = () => attachIframe(iframe)
    iframe?.addEventListener('load', onLoad)
    attachIframe(iframe)

    const observer = container
      ? new MutationObserver(() => {
          const next = container.querySelector('iframe') as HTMLIFrameElement | null
          if (next === iframe) {
            attachIframe(iframe)
            return
          }
          iframe?.removeEventListener('load', onLoad)
          iframe = next
          onLoad = () => attachIframe(iframe)
          iframe?.addEventListener('load', onLoad)
          attachIframe(iframe)
        })
      : null
    if (container) observer?.observe(container, { childList: true, subtree: true })

    return () => {
      documents.forEach((doc) =>
        doc.removeEventListener('keydown', handleKeyDown, true)
      )
      observer?.disconnect()
      iframe?.removeEventListener('load', onLoad)
    }
  }, [containerRef, copyRemoteSelection, enabled, pasteIntoRemote, sessionId])

  return { errorMessageKey, copyRemoteSelection, pasteIntoRemote }
}
