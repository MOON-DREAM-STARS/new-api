import { act, render, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { createWebWorkspaceKasmTicket } from '../../api'
import {
  buildKasmClientUrl,
  useRemoteSurface,
  type RemoteSurfaceController,
} from '../use-remote-surface'

vi.mock('../../api', () => ({
  createWebWorkspaceKasmTicket: vi.fn(),
}))

const createTicket = vi.mocked(createWebWorkspaceKasmTicket)

let controller: RemoteSurfaceController | null = null

function Harness() {
  const surface = useRemoteSurface({ sessionId: 'session-1', enabled: true })
  controller = surface
  return <div ref={surface.containerRef} data-testid='surface' />
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
    createTicket.mockResolvedValue('ticket-1')
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

    createTicket.mockResolvedValue('ticket-2')
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
  test('locks the Kasm ticket, clipboard, IME, and low-latency profile', () => {
    const url = new URL(
      'http://localhost' +
        buildKasmClientUrl('session value', 'ticket/value')
    )
    expect(url.pathname).toContain('/session/session%20value/kasm/t/ticket%2Fvalue/vnc.html')
    expect(Array.from(url.searchParams.entries())).toEqual([
      ['autoconnect', '1'],
      ['resize', 'scale'],
      [
        'path',
        'api/web-workspace/session/session%20value/kasm/t/ticket%2Fvalue/websockify',
      ],
      ['clipboard_up', '0'],
      ['clipboard_down', '0'],
      ['clipboard_seamless', '0'],
      ['enable_ime', '0'],
      ['quality', '6'],
      ['dynamic_quality_min', '6'],
      ['dynamic_quality_max', '8'],
      ['treat_lossless', '8'],
      ['framerate', '30'],
      ['jpeg_video_quality', '6'],
      ['webp_video_quality', '6'],
      ['video_area', '65'],
      ['video_time', '5'],
      ['video_out_time', '3'],
      ['video_scaling', '1'],
      ['max_video_resolution_x', '1920'],
      ['max_video_resolution_y', '1080'],
    ])
    for (const key of ['webrtc', 'h264']) {
      expect(url.searchParams.has(key)).toBe(false)
    }
  })
})
