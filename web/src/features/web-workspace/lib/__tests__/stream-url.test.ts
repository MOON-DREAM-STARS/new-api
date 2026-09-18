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
import { describe, expect, test } from 'vitest'

import { buildStreamWebSocketUrl } from '../stream-url'

const STREAM_PATH = '/api/web-workspace/session/session-1/stream'

describe('buildStreamWebSocketUrl', () => {
  test('uses wss on an https dashboard and appends the ticket', () => {
    const url = buildStreamWebSocketUrl(STREAM_PATH, 'ticket-1', {
      protocol: 'https:',
      host: 'api.example.com',
    })
    expect(url).toBe(
      'wss://api.example.com/api/web-workspace/session/session-1/stream?ticket=ticket-1'
    )
  })

  test('uses ws on a plain http dashboard', () => {
    const url = buildStreamWebSocketUrl(STREAM_PATH, 'ticket-2', {
      protocol: 'http:',
      host: 'localhost:3000',
    })
    expect(url.startsWith('ws://localhost:3000/api/web-workspace/')).toBe(true)
  })

  test('never follows an absolute stream url to a foreign host', () => {
    const url = buildStreamWebSocketUrl(
      'https://browser-agent.internal/session/1/stream',
      'ticket-3',
      { protocol: 'https:', host: 'api.example.com' }
    )
    expect(url).toBe('wss://api.example.com/session/1/stream?ticket=ticket-3')
  })

  test('encodes tickets that contain reserved characters', () => {
    const url = buildStreamWebSocketUrl(STREAM_PATH, 'a+b&c=d', {
      protocol: 'https:',
      host: 'api.example.com',
    })
    expect(url.endsWith('?ticket=a%2Bb%26c%3Dd')).toBe(true)
  })
})
