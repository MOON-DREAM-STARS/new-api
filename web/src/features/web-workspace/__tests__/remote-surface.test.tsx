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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { RemoteSurface } from '../components/remote-surface'

const novnc = vi.hoisted(() => {
  type Listener = (event: Event) => void
  class MockRFB {
    static instances: MockRFB[] = []
    readonly listeners = new Map<string, Listener[]>()
    readonly target: Element
    readonly channel: unknown
    readonly options: Record<string, unknown>
    disconnected = false

    constructor(
      target: Element,
      channel: unknown,
      options?: Record<string, unknown>
    ) {
      this.target = target
      this.channel = channel
      this.options = options ?? {}
      MockRFB.instances.push(this)
    }

    addEventListener(type: string, listener: Listener) {
      const existing = this.listeners.get(type) ?? []
      this.listeners.set(type, [...existing, listener])
    }

    removeEventListener(type: string, listener: Listener) {
      const existing = this.listeners.get(type) ?? []
      this.listeners.set(
        type,
        existing.filter((item) => item !== listener)
      )
    }

    disconnect() {
      this.disconnected = true
    }

    focus() {
      /* focus is not part of the behavioural assertions here */
    }

    emit(type: string) {
      for (const listener of this.listeners.get(type) ?? []) {
        listener(new Event(type))
      }
    }
  }
  return { MockRFB }
})

vi.mock('@novnc/novnc', () => ({ default: novnc.MockRFB }))

class MockWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  static instances: MockWebSocket[] = []

  readonly url: string
  readyState = MockWebSocket.OPEN
  binaryType = 'blob'
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
  }
}

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { post: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalPost = apiClient.post
const originalWebSocket = globalThis.WebSocket

const TICKET_PATH = '/api/web-workspace/session/session-1/stream-ticket'

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function installTicketApi() {
  let calls = 0
  apiClient.post = async (url) => {
    expect(url).toBe(TICKET_PATH)
    calls += 1
    return ok({
      ticket: ['ticket', String(calls)].join('-'),
      expires_at: Math.floor(Date.now() / 1000) + 60,
      stream_url: '/api/web-workspace/session/session-1/stream',
    })
  }
  return {
    get calls() {
      return calls
    },
  }
}

async function flushMicrotasks() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  novnc.MockRFB.instances.length = 0
  MockWebSocket.instances = []
  globalThis.WebSocket = MockWebSocket as unknown as typeof WebSocket
})

afterEach(() => {
  apiClient.post = originalPost
  globalThis.WebSocket = originalWebSocket
  vi.useRealTimers()
})
function latestRfb() {
  const instance = novnc.MockRFB.instances.at(-1)
  if (!instance) throw new Error('No RFB instance was attached.')
  return instance
}

function renderSurface(enabled = true) {
  return render(
    <RemoteSurface
      sessionId='session-1'
      sessionState='RUNNING'
      enabled={enabled}
    />
  )
}

describe('RemoteSurface', () => {
  test('attaches a ticket-backed websocket to noVNC on the same origin', async () => {
    installTicketApi()
    renderSurface()

    await waitFor(() => {
      expect(novnc.MockRFB.instances).toHaveLength(1)
    })
    const rfb = novnc.MockRFB.instances[0]
    expect(rfb.target).toBe(screen.getByTestId('web-workspace-surface'))
    expect(rfb.options.scaleViewport).toBe(true)
    expect(rfb.options.viewOnly).toBe(false)
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toContain(
      '/api/web-workspace/session/session-1/stream?ticket=ticket-1'
    )
  })

  test('reports the connected state announced by noVNC', async () => {
    installTicketApi()
    renderSurface()

    await waitFor(() => {
      expect(novnc.MockRFB.instances).toHaveLength(1)
    })
    act(() => {
      novnc.MockRFB.instances[0].emit('connect')
    })

    expect(
      screen.getByText('Connected to the remote browser.')
    ).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
  })

  test('does not start a stream while the session is not running', () => {
    installTicketApi()
    render(
      <RemoteSurface sessionId='session-1' sessionState='STOPPED' enabled />
    )

    expect(
      screen.getByText(
        'The remote browser is unavailable while the session is not running.'
      )
    ).toBeInTheDocument()
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  test('disables the remote surface below the desktop breakpoint', () => {
    installTicketApi()
    renderSurface(false)

    expect(
      screen.getByText(
        'The remote browser is disabled on small screens. Use a desktop viewport at least 1024px wide.'
      )
    ).toBeInTheDocument()
    expect(screen.queryByTestId('web-workspace-surface')).toBeNull()
    expect(MockWebSocket.instances).toHaveLength(0)
  })
  test('reconnects with a fresh ticket and reports the attempt', async () => {
    const tickets = installTicketApi()
    renderSurface()

    await waitFor(() => {
      expect(novnc.MockRFB.instances).toHaveLength(1)
    })

    vi.useFakeTimers()
    act(() => {
      novnc.MockRFB.instances[0].emit('disconnect')
    })
    expect(screen.getByRole('status').textContent).toContain(
      'Reconnecting (attempt 1 of 5)...'
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    await flushMicrotasks()

    expect(novnc.MockRFB.instances).toHaveLength(2)
    expect(tickets.calls).toBe(2)
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  test('stops retrying at the attempt limit and offers a manual reconnect', async () => {
    const tickets = installTicketApi()
    renderSurface()

    await waitFor(() => {
      expect(novnc.MockRFB.instances).toHaveLength(1)
    })

    vi.useFakeTimers()
    for (const delay of [1000, 2000, 4000, 8000, 15000]) {
      const current = latestRfb()
      act(() => {
        current.emit('disconnect')
      })
      act(() => {
        vi.advanceTimersByTime(delay)
      })
      await flushMicrotasks()
    }
    expect(novnc.MockRFB.instances).toHaveLength(6)

    const last = latestRfb()
    act(() => {
      last.emit('disconnect')
    })

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Could not connect to the remote browser.'
    )
    vi.useRealTimers()

    const manual = screen.getAllByRole('button', { name: 'Reconnect now' })[0]
    await userEvent.click(manual)

    await waitFor(() => {
      expect(tickets.calls).toBe(7)
    })
  })

  test('enters and exits fullscreen for the surface container', async () => {
    const requestFullscreen = vi.fn(() => Promise.resolve())
    const exitFullscreen = vi.fn(() => Promise.resolve())
    Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: requestFullscreen,
    })
    Object.defineProperty(document, 'exitFullscreen', {
      configurable: true,
      value: exitFullscreen,
    })

    installTicketApi()
    renderSurface()

    await userEvent.click(
      await screen.findByRole('button', { name: 'Enter fullscreen' })
    )
    expect(requestFullscreen).toHaveBeenCalledTimes(1)

    const surface = screen.getByTestId('web-workspace-surface')
    Object.defineProperty(document, 'fullscreenElement', {
      configurable: true,
      get: () => surface,
    })
    act(() => {
      document.dispatchEvent(new Event('fullscreenchange'))
    })

    expect(
      await screen.findByRole('button', { name: 'Exit fullscreen' })
    ).toBeInTheDocument()
    expect(
      screen.getByText('Press Esc to exit fullscreen.')
    ).toBeInTheDocument()

    Reflect.deleteProperty(HTMLElement.prototype, 'requestFullscreen')
    Reflect.deleteProperty(document, 'exitFullscreen')
    Reflect.deleteProperty(document, 'fullscreenElement')
  })
})
