import { describe, expect, test } from 'vitest'

import { buildKasmClientUrl } from '../use-remote-surface'

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
