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
export type StreamOrigin = {
  protocol: string
  host: string
}

/**
 * Turns the same-origin API stream path into a same-origin ws/wss URL and
 * attaches the one-time ticket. The scheme always follows the page location so
 * a TLS dashboard never downgrades to an insecure socket.
 */
export function buildStreamWebSocketUrl(
  streamPath: string,
  ticket: string,
  origin: StreamOrigin
): string {
  const scheme = origin.protocol === 'https:' ? 'wss' : 'ws'
  const url = new URL(streamPath, `${scheme}://${origin.host}`)
  url.protocol = `${scheme}:`
  url.host = origin.host
  url.searchParams.set('ticket', ticket)
  return url.toString()
}
