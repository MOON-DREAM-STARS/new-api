import { act, render, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { createWebWorkspaceStreamTicket } from '../../api'
import {
  buildKasmClientUrl,
  useRemoteSurface,
  type RemoteSurfaceController,
} from '../use-remote-surface'

vi.mock('../../api', () => ({
  createWebWorkspaceStreamTicket: vi.fn(),
}))

const createTicket = vi.mocked(createWebWorkspaceStreamTicket)

let controller: RemoteSurfaceController | null = null

function Harness() {
  const surface = useRemoteSurface({ sessionId: 'session-1', enabled: true })
  controller = surface
  return <div ref={surface.containerRef} data-testid='surface' />
}

function ticket(id: string) {
  return { ticket: id, expires_at: 1, stream_url: `/stream/${id}` }
}

async function reportConnected(frame: HTMLIFrameElement) {
  await act(async () => {
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { action: 'connection_state', value: 'connected' },
        origin: window.location.origin,
        source: frame.contentWindow,
      })
    )
  })
}

afterEach(() => {
  controller = null
  createTicket.mockReset()
})

describe('useRemoteSurface', () => {
  test('keeps reporting connection state after a reconnect reuses the iframe', async () => {
    createTicket.mockResolvedValue(ticket('ticket-1') as never)
    render(<Harness />)

    const frame = await waitFor(() => {
      const mounted = document.querySelector<HTMLIFrameElement>(
        'iframe[data-testid="web-workspace-surface-iframe"]'
      )
      if (!mounted) throw new Error('surface iframe is not mounted yet')
      return mounted
    })

    await reportConnected(frame)
    await waitFor(() => expect(controller?.status).toBe('connected'))

    createTicket.mockResolvedValue(ticket('ticket-2') as never)
    await act(async () => {
      controller?.reconnect()
    })

    await waitFor(() =>
      expect(frame.getAttribute('src')).toContain('ticket-2')
    )
    expect(controller?.status).toBe('connecting')

    await reportConnected(frame)
    await waitFor(() => expect(controller?.status).toBe('connected'))
  })
})

describe('buildKasmClientUrl', () => {
  test('keeps the ticket path and disables KasmVNC clipboard and IME', () => {
    const url = new URL(
      'http://localhost' +
        buildKasmClientUrl('session value', 'ticket/value')
    )
    expect(url.pathname).toContain('/session/session%20value/kasm/t/ticket%2Fvalue/vnc.html')
    expect(url.searchParams.get('path')).toBe(
      'api/web-workspace/session/session%20value/kasm/t/ticket%2Fvalue/websockify'
    )
    expect(url.searchParams.get('clipboard_up')).toBe('0')
    expect(url.searchParams.get('clipboard_down')).toBe('0')
    expect(url.searchParams.get('clipboard_seamless')).toBe('0')
    expect(url.searchParams.has('enable_ime')).toBe(false)
  })
})
