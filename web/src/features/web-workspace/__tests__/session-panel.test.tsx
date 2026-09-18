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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { SessionPanel } from '../components/session-panel'
import type { WebWorkspaceSession } from '../types'

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod; post: ApiMethod; delete: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalDelete = apiClient.delete

const SESSION_PATH = '/api/web-workspace/session'

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function fail(status: number, code: string) {
  return Promise.reject({
    response: { status, data: { success: false, code } },
  })
}

function sessionFixture(
  state: string,
  overrides: Partial<WebWorkspaceSession> = {}
): WebWorkspaceSession {
  return {
    session_id: 'session-1',
    state,
    created_at: 1_700_000_000,
    last_seen_at: 1_700_000_000,
    idle_deadline_at: Math.floor(Date.now() / 1000) + 120,
    ...overrides,
  }
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.delete = originalDelete
})

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <SessionPanel />
    </QueryClientProvider>
  )
}
describe('SessionPanel', () => {
  test('announces a live session with its idle countdown and controls', async () => {
    apiClient.get = async (url) => {
      expect(url).toBe(SESSION_PATH)
      return ok(sessionFixture('RUNNING'))
    }
    renderPanel()

    const status = await screen.findByRole('status')
    expect(status).toHaveAttribute('aria-live', 'polite')
    await waitFor(() => {
      expect(status.textContent).toContain('Browser session is')
    })
    expect(status.textContent).toContain('Running')
    expect(screen.getByText('Idle shutdown in 2:00.')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Stop session' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Restart session' })
    ).toBeInTheDocument()
  })

  test('starts a session from the empty state', async () => {
    let session: WebWorkspaceSession | null = null
    apiClient.get = async () => ok(session)
    apiClient.post = async (url) => {
      expect(url).toBe(SESSION_PATH)
      session = sessionFixture('STARTING')
      return ok(session)
    }
    renderPanel()

    expect(
      await screen.findByText('No browser session is running.')
    ).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Start session' }))

    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toContain('Starting')
    })
    expect(
      screen.getByRole('button', { name: 'Stop session' })
    ).toBeInTheDocument()
  })

  test('stops the running session', async () => {
    let session: WebWorkspaceSession | null = sessionFixture('RUNNING')
    apiClient.get = async () => ok(session)
    apiClient.delete = async (url) => {
      expect(url).toBe('/api/web-workspace/session/session-1')
      session = null
      return ok({ id: 'session-1' })
    }
    renderPanel()

    await screen.findByRole('button', { name: 'Stop session' })
    await userEvent.click(screen.getByRole('button', { name: 'Stop session' }))

    expect(
      await screen.findByText('No browser session is running.')
    ).toBeInTheDocument()
  })

  test('surfaces a session request failure with a retry entry', async () => {
    let attempts = 0
    apiClient.get = async () => {
      attempts += 1
      if (attempts === 1) return fail(503, 'WEB_WORKSPACE_AGENT_UNAVAILABLE')
      return ok(sessionFixture('RUNNING'))
    }
    renderPanel()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Browser session unavailable')
    expect(alert).toHaveTextContent(
      'The browser agent is unavailable right now. Try again in a moment.'
    )

    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toContain('Running')
    })
  })

  test('offers a start entry when the runtime failed', async () => {
    apiClient.get = async () => ok(sessionFixture('FAILED'))
    renderPanel()

    expect(await screen.findByText('Failed')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Start session' })
    ).toBeInTheDocument()
  })
})
