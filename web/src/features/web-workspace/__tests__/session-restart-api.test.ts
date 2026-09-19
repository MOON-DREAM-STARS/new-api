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
*/import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { restartWebWorkspaceSession } from '../api'

type ApiPost = (url: string, data?: unknown) => Promise<{ data: unknown }>

const apiClient = api as unknown as { post: ApiPost }
const originalPost = apiClient.post

afterEach(() => {
  apiClient.post = originalPost
})

function recordPosts(posts: Array<{ url: string; data: unknown }>) {
  apiClient.post = async (url, data) => {
    posts.push({ url, data })
    return { data: { success: true, message: '', data: { session_id: 's1' } } }
  }
}

describe('restartWebWorkspaceSession', () => {
  test('locks the runtime through the restart endpoint', async () => {
    const posts: Array<{ url: string; data: unknown }> = []
    recordPosts(posts)

    await restartWebWorkspaceSession('s1', { mode: 'LOCKED' })

    expect(posts).toEqual([
      { url: '/api/web-workspace/session/s1/restart', data: { mode: 'LOCKED' } },
    ])
  })

  test('restarts without a mode override and encodes the session id', async () => {
    const posts: Array<{ url: string; data: unknown }> = []
    recordPosts(posts)

    await restartWebWorkspaceSession('s 1/2')

    expect(posts).toEqual([
      {
        url: '/api/web-workspace/session/s%201%2F2/restart',
        data: { mode: '' },
      },
    ])
  })
})