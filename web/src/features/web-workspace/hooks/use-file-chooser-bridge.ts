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
import { useEffect, useState } from 'react'

import {
  cancelWebWorkspaceFileChooser,
  fetchWebWorkspaceFileChooser,
  uploadWebWorkspaceFileChooserFiles,
} from '../api'
import { WEB_WORKSPACE_FILE_CHOOSER_WAIT_MS } from '../constants'
import { classifyWebWorkspaceError } from '../lib/errors'

export type UseFileChooserBridgeOptions = {
  sessionId: string | null
  enabled: boolean
}

export type FileChooserBridgeController = {
  errorMessageKey: string | null
}

function isAbortError(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const value = error as { name?: unknown; code?: unknown }
  return value.name === 'AbortError' || value.code === 'ERR_CANCELED'
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

/**
 * Bridges the remote ChatGPT paperclip to the operator's local file picker.
 *
 * The hook long-polls for one pending chooser, creates a real hidden file input
 * on the local page, streams the selected files as multipart FormData, and
 * cancels the guard command when the picker is dismissed. Stable control-plane
 * errors are classified through the shared Web Workspace error path.
 */
export function useFileChooserBridge(
  options: UseFileChooserBridgeOptions
): FileChooserBridgeController {
  const [errorMessageKey, setErrorMessageKey] = useState<string | null>(null)

  useEffect(() => {
    const sessionId = options.sessionId
    if (!options.enabled || !sessionId || typeof document === 'undefined') {
      setErrorMessageKey(null)
      return undefined
    }

    let stopped = false
    let abortController: AbortController | null = null
    let input: HTMLInputElement | null = null
    let resolveActive: (() => void) | null = null

    const finishActive = () => {
      resolveActive?.()
      resolveActive = null
    }

    const stopInput = (cancel: boolean, resolveImmediately = true) => {
      const current = input
      if (!current) {
        if (resolveImmediately) finishActive()
        return
      }
      input = null
      const chooserID = current.dataset.chooserId ?? ''
      current.remove()
      if (cancel && chooserID) {
        void cancelWebWorkspaceFileChooser(sessionId, chooserID)
          .catch((error) => {
            if (!stopped && !isAbortError(error)) {
              setErrorMessageKey(
                classifyWebWorkspaceError(error).messageKey
              )
            }
          })
          .finally(finishActive)
        return
      }
      if (resolveImmediately) finishActive()
    }

    const openPicker = (chooser: {
      chooser_id: string
      mode: 'selectSingle' | 'selectMultiple'
    }) =>
      new Promise<void>((resolve) => {
        resolveActive = resolve
        const next = document.createElement('input')
        next.type = 'file'
        next.multiple = chooser.mode === 'selectMultiple'
        next.hidden = true
        next.dataset.chooserId = chooser.chooser_id
        next.setAttribute('aria-hidden', 'true')
        input = next
        next.oncancel = () => stopInput(true)
        next.onchange = () => {
          const selected = next.files ? Array.from(next.files) : []
          const chooserID = chooser.chooser_id
          stopInput(false, false)
          if (!selected.length) {
            finishActive()
            return
          }
          const formData = new FormData()
          for (const file of selected) {
            formData.append('files', file, file.name)
          }
          void uploadWebWorkspaceFileChooserFiles(
            sessionId,
            chooserID,
            formData
          )
            .then(() => {
              if (!stopped) setErrorMessageKey(null)
            })
            .catch((error) => {
              if (!stopped && !isAbortError(error)) {
                setErrorMessageKey(
                  classifyWebWorkspaceError(error).messageKey
                )
              }
            })
            .finally(finishActive)
        }
        document.body.appendChild(next)
        next.click()
      })

    const poll = async () => {
      while (!stopped) {
        try {
          abortController = new AbortController()
          const chooser = await fetchWebWorkspaceFileChooser(
            sessionId,
            WEB_WORKSPACE_FILE_CHOOSER_WAIT_MS,
            abortController.signal
          )
          abortController = null
          if (stopped) return
          if (!chooser) continue
          setErrorMessageKey(null)
          await openPicker(chooser)
        } catch (error) {
          abortController = null
          if (stopped || isAbortError(error)) return
          setErrorMessageKey(classifyWebWorkspaceError(error).messageKey)
          await wait(1000)
        }
      }
    }

    void poll()
    return () => {
      stopped = true
      abortController?.abort()
      stopInput(true)
    }
  }, [options.enabled, options.sessionId])

  return { errorMessageKey }
}
