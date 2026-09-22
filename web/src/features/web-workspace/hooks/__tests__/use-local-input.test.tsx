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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { RefObject } from 'react'
import { act, useMemo, useRef } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  dispatchWebWorkspaceInputKey,
  getWebWorkspaceInputCaret,
  insertWebWorkspaceInputText,
} from '../../api'
import type { RemoteSurfaceController } from '../use-remote-surface'
import {
  useLocalInput,
  type LocalInputController,
} from '../use-local-input'

vi.mock('../../api', () => ({
  dispatchWebWorkspaceInputKey: vi.fn(),
  getWebWorkspaceInputCaret: vi.fn(),
  insertWebWorkspaceInputText: vi.fn(),
}))

const insertText = vi.mocked(insertWebWorkspaceInputText)
const getCaret = vi.mocked(getWebWorkspaceInputCaret)
const dispatchKey = vi.mocked(dispatchWebWorkspaceInputKey)

function makeSurface(): RemoteSurfaceController {
  return {
    containerRef: { current: null } as RefObject<HTMLDivElement | null>,
    screen: null,
    status: 'connected',
    attempt: 0,
    maxAttempts: 5,
    errorMessageKey: null,
    getIframe: () => null,
    getCanvasMetrics: () => null,
    focusSurface: vi.fn(),
    reconnect: vi.fn(),
  }
}

function LocalInputHarness(props: {
  onReady: (controller: LocalInputController) => void
}) {
  const controller = useLocalInput({
    sessionId: 'session-1',
    enabled: true,
    surface: makeSurface(),
  })
  props.onReady(controller)
  return (
    <input
      ref={controller.anchorRef}
      aria-label='Local input'
      onCompositionStart={controller.handleCompositionStart}
      onCompositionEnd={controller.handleCompositionEnd}
      onInput={controller.handleInput}
      onKeyDown={controller.handleKeyDown}
    />
  )
}

function IframeLocalInputHarness(props: {
  onReady: (controller: LocalInputController) => void
}) {
  const frameRef = useRef<HTMLIFrameElement>(null)
  const surface = useMemo(
    () => ({ ...makeSurface(), getIframe: () => frameRef.current }),
    []
  )
  const controller = useLocalInput({
    sessionId: 'session-1',
    enabled: true,
    surface,
  })
  props.onReady(controller)
  return (
    <div>
      <iframe ref={frameRef} title='Remote frame' />
      <input ref={controller.anchorRef} aria-label='Local input' />
    </div>
  )
}

afterEach(() => {
  vi.useRealTimers()
  insertText.mockReset()
  getCaret.mockReset()
  dispatchKey.mockReset()
})

async function flushMicrotasks() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

describe('useLocalInput', () => {
  test('injects compositionend once and skips the trailing input event', async () => {
    getCaret.mockResolvedValue(null)
    insertText.mockResolvedValue(undefined)
    const controllerRef: { current: LocalInputController | null } = { current: null }
    const view = render(
      <LocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )
    const input = view.getByRole('textbox', { name: 'Local input' }) as HTMLInputElement
    act(() => controllerRef.current?.toggle())

    fireEvent.compositionStart(input)
    input.value = '你好'
    fireEvent.compositionEnd(input, { data: '你好' })
    fireEvent.input(input)

    await waitFor(() => expect(insertText).toHaveBeenCalledTimes(1))
    expect(insertText).toHaveBeenCalledWith('session-1', '你好')
    expect(input.value).toBe('')
  })

  test('takes the keyboard back when the remote page is clicked', async () => {
    getCaret.mockResolvedValue(null)
    insertText.mockResolvedValue(undefined)
    const controllerRef: { current: LocalInputController | null } = { current: null }
    render(
      <IframeLocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )

    const anchor = screen.getByRole('textbox', { name: 'Local input' })
    const frame = screen.getByTitle('Remote frame') as HTMLIFrameElement
    await waitFor(() => expect(frame.contentDocument).toBeTruthy())

    act(() => controllerRef.current?.toggle())
    await waitFor(() => expect(document.activeElement).toBe(anchor))
    await waitFor(() => expect(getCaret).toHaveBeenCalledTimes(1))

    // The operator clicks the remote page to place its caret.
    getCaret.mockClear()
    anchor.blur()
    expect(document.activeElement).not.toBe(anchor)
    act(() => {
      frame.contentDocument!.dispatchEvent(
        new MouseEvent('click', { bubbles: true })
      )
    })

    await waitFor(() => expect(document.activeElement).toBe(anchor))
    await waitFor(() => expect(getCaret).toHaveBeenCalledTimes(1))
  })

  test('debounces caret refresh after success and cancels the pending refresh', async () => {
    vi.useFakeTimers()
    getCaret.mockResolvedValue(null)
    insertText.mockResolvedValue(undefined)
    const controllerRef: { current: LocalInputController | null } = { current: null }
    const view = render(
      <LocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )
    const input = view.getByRole('textbox', { name: 'Local input' }) as HTMLInputElement
    act(() => controllerRef.current?.toggle())
    await flushMicrotasks()
    expect(getCaret).toHaveBeenCalledTimes(1)
    getCaret.mockClear()

    input.value = 'first'
    fireEvent.input(input)
    await flushMicrotasks()
    expect(insertText).toHaveBeenNthCalledWith(1, 'session-1', 'first')
    expect(getCaret).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100)
    })
    input.value = 'second'
    fireEvent.input(input)
    await flushMicrotasks()
    expect(insertText).toHaveBeenNthCalledWith(2, 'session-1', 'second')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(149)
    })
    expect(getCaret).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(getCaret).toHaveBeenCalledTimes(1)
  })

  test('does not replay a caret failure over successful text injection', async () => {
    vi.useFakeTimers()
    getCaret.mockResolvedValue(null)
    insertText.mockResolvedValue(undefined)
    const controllerRef: { current: LocalInputController | null } = { current: null }
    const view = render(
      <LocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )
    const input = view.getByRole('textbox', { name: 'Local input' }) as HTMLInputElement
    act(() => controllerRef.current?.toggle())
    await flushMicrotasks()

    getCaret.mockReset()
    getCaret.mockRejectedValue(new Error('caret probe failed'))
    input.value = '已注入'
    fireEvent.input(input)
    await flushMicrotasks()
    expect(insertText).toHaveBeenCalledWith('session-1', '已注入')
    expect(input.value).toBe('')
    expect(controllerRef.current?.errorCode).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150)
    })
    await flushMicrotasks()
    expect(input.value).toBe('')
    expect(controllerRef.current?.errorCode).toBeNull()
  })

  test('ignores a stale caret failure after a newer text injection succeeds', async () => {
    vi.useFakeTimers()
    let rejectCaret!: (error: Error) => void
    getCaret.mockResolvedValue(null)
    getCaret.mockImplementationOnce(
      () =>
        new Promise<never>((_, reject) => {
          rejectCaret = reject
        })
    )
    insertText.mockResolvedValue(undefined)
    const controllerRef: { current: LocalInputController | null } = { current: null }
    const view = render(
      <LocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )
    const input = view.getByRole('textbox', { name: 'Local input' }) as HTMLInputElement
    act(() => controllerRef.current?.toggle())

    input.value = '新文本'
    fireEvent.input(input)
    await flushMicrotasks()
    expect(insertText).toHaveBeenCalledWith('session-1', '新文本')
    expect(input.value).toBe('')

    await act(async () => {
      rejectCaret(new Error('stale caret probe failed'))
      await Promise.resolve()
    })
    expect(controllerRef.current?.errorCode).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150)
    })
    expect(controllerRef.current?.errorCode).toBeNull()
  })

  test('keeps failed committed text visible and reports the real error code', async () => {
    getCaret.mockResolvedValue(null)
    insertText.mockRejectedValue({
      response: {
        status: 422,
        data: { success: false, code: 'WEB_WORKSPACE_INPUT_REJECTED' },
      },
    })
    const controllerRef: { current: LocalInputController | null } = { current: null }
    const view = render(
      <LocalInputHarness
        onReady={(next) => {
          controllerRef.current = next
        }}
      />
    )
    const input = view.getByRole('textbox', { name: 'Local input' }) as HTMLInputElement
    act(() => controllerRef.current?.toggle())

    input.value = '失败文本'
    fireEvent.input(input)

    await waitFor(() => expect(insertText).toHaveBeenCalledTimes(1))
    expect(input.value).toBe('失败文本')
    expect(controllerRef.current?.errorCode).toBe(
      'WEB_WORKSPACE_INPUT_REJECTED'
    )
  })
})
