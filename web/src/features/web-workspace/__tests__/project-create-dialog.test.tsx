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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { ProjectCreateDialog } from '../components/project-create-dialog'

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { post: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalPost = apiClient.post

const PERMIT_PATH = '/api/web-workspace/projects'
const SESSION_PATH = '/api/web-workspace/session'

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function fail(status: number, code: string) {
  return Promise.reject({
    response: { status, data: { success: false, code } },
  })
}

function futureExpiry(secondsFromNow: number): number {
  return Math.floor(Date.now() / 1000) + secondsFromNow
}

afterEach(() => {
  apiClient.post = originalPost
})

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ProjectCreateDialog open onOpenChange={() => undefined} />
    </QueryClientProvider>
  )
}

describe('ProjectCreateDialog', () => {
  test('issues a permit and explains the remote browser flow', async () => {
    let permitCalls = 0
    apiClient.post = async (url) => {
      expect(url).toBe(PERMIT_PATH)
      permitCalls += 1
      return ok({ permit_id: 'permit-1', expires_at: futureExpiry(300) })
    }
    renderDialog()

    await userEvent.click(
      screen.getByRole('button', { name: 'Issue creation permit' })
    )

    expect(await screen.findByText(/Permit expires in/)).toBeInTheDocument()
    expect(
      screen.getByText(/Open the remote browser and create the project there/)
    ).toBeInTheDocument()
    expect(permitCalls).toBe(1)
  })

  test('explains a missing session and offers starting one', async () => {
    let sessionStarted = false
    apiClient.post = async (url) => {
      if (url === SESSION_PATH) {
        sessionStarted = true
        return ok({
          session_id: 'session-1',
          state: 'RUNNING',
          created_at: 1,
          last_seen_at: 1,
          idle_deadline_at: futureExpiry(600),
        })
      }
      if (sessionStarted) {
        return ok({ permit_id: 'permit-2', expires_at: futureExpiry(300) })
      }
      return fail(409, 'WEB_WORKSPACE_SESSION_REQUIRED')
    }
    renderDialog()

    await userEvent.click(
      screen.getByRole('button', { name: 'Issue creation permit' })
    )
    expect(
      await screen.findByText(
        'Start a browser session before creating a project.'
      )
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Start session' }))
    expect(sessionStarted).toBe(true)

    // The session now exists, so the permit can be issued.
    await userEvent.click(
      await screen.findByRole('button', { name: 'Issue creation permit' })
    )

    expect(await screen.findByText(/Permit expires in/)).toBeInTheDocument()
  })

  test('explains the project limit conflict without pretending success', async () => {
    apiClient.post = async () => fail(409, 'WEB_WORKSPACE_PROJECT_LIMIT')
    renderDialog()

    await userEvent.click(
      screen.getByRole('button', { name: 'Issue creation permit' })
    )

    expect(await screen.findByText('Project limit reached')).toBeInTheDocument()
    expect(
      screen.getByText(
        'You have reached the project limit. Remove an existing project first.'
      )
    ).toBeInTheDocument()
  })

  test('offers a retry when the browser agent is unavailable', async () => {
    let attempts = 0
    apiClient.post = async () => {
      attempts += 1
      if (attempts === 1) {
        return fail(503, 'WEB_WORKSPACE_AGENT_UNAVAILABLE')
      }
      return ok({ permit_id: 'permit-3', expires_at: futureExpiry(300) })
    }
    renderDialog()

    await userEvent.click(
      screen.getByRole('button', { name: 'Issue creation permit' })
    )
    expect(
      await screen.findByText(
        'The browser agent is unavailable right now. Try again in a moment.'
      )
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText(/Permit expires in/)).toBeInTheDocument()
    expect(attempts).toBe(2)
  })

  test('asks for a new permit once the issued permit has expired', async () => {
    apiClient.post = async () =>
      ok({ permit_id: 'permit-4', expires_at: futureExpiry(-5) })
    renderDialog()

    await userEvent.click(
      screen.getByRole('button', { name: 'Issue creation permit' })
    )

    expect(
      await screen.findByText('Creation permit expired.')
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Issue a new permit' })
    ).toBeInTheDocument()
  })
})
