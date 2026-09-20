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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { WEB_WORKSPACE_SESSION_QUERY_KEY } from '../constants'
import { ProjectCreateDialog } from '../components/project-create-dialog'
import type { WebWorkspaceProjectCreation } from '../types'

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
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

function mockEmptySession() {
  apiClient.get = async (url) => {
    if (url === SESSION_PATH) return ok(sessionDto(null))
    if (url === PERMIT_PATH) return ok({ items: [] })
    throw new Error(`unexpected get ${url}`)
  }
}

function sessionDto(projectCreation: WebWorkspaceProjectCreation | null) {
  return {
    session_id: 'session-1',
    state: 'RUNNING',
    mode: 'LOCKED',
    created_at: 1,
    last_seen_at: 1,
    idle_deadline_at: futureExpiry(600),
    stream_bytes_out: 0,
    stream_bytes_in: 0,
    navigation: null,
    page: null,
    project_creation: projectCreation,
  }
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
})

function renderDialog(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <ProjectCreateDialog open onOpenChange={onOpenChange} />
    </QueryClientProvider>
  )
  return { ...result, onOpenChange, queryClient }
}

async function waitForCreateButton() {
  const button = await screen.findByRole('button', { name: 'Create project' })
  await waitFor(() => expect(button).toBeEnabled())
  return button
}

describe('ProjectCreateDialog', () => {
  test('requires a non-blank project name before issuing a permit', async () => {
    mockEmptySession()

    renderDialog()

    const input = screen.getByLabelText('Project name')
    const submit = screen.getByRole('button', { name: 'Create project' })

    // The field starts empty: the hint only appears once the operator has
    // interacted with it, so an untouched dialog shows no error.
    expect(submit).toBeDisabled()
    expect(
      screen.queryByText('Enter a project name of 1-64 characters.')
    ).not.toBeInTheDocument()

    await userEvent.type(input, '   ')
    expect(submit).toBeDisabled()
    expect(
      screen.getByText('Enter a project name of 1-64 characters.')
    ).toBeInTheDocument()

    await userEvent.clear(input)
    await userEvent.type(input, 'Alpha')
    await waitFor(() => {
      expect(submit).toBeEnabled()
    })
    expect(
      screen.queryByText('Enter a project name of 1-64 characters.')
    ).not.toBeInTheDocument()
  })

  test('issues a permit and shows the real creation state', async () => {
    let permitPayload: unknown
    let permitCalls = 0
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) return ok(sessionDto(null))
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async (url, data) => {
      expect(url).toBe(PERMIT_PATH)
      permitPayload = data
      permitCalls += 1
      return ok({ permit_id: 'permit-1', expires_at: futureExpiry(300) })
    }
    renderDialog()

    await userEvent.type(screen.getByLabelText('Project name'), ' Alpha ')
    await userEvent.click(await waitForCreateButton())

    expect(
      await screen.findByText('Creating the project in the remote browser...')
    ).toBeInTheDocument()
    expect(permitCalls).toBe(1)
    expect(permitPayload).toEqual({ name: 'Alpha' })
  })

  test('closes the dialog when the guard observes the created project', async () => {
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) {
        return ok(
          sessionDto({
            permit_id: 'permit-1',
            state: 'CREATED',
            error: '',
            updated_at: 2,
          })
        )
      }
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async () =>
      ok({ permit_id: 'permit-1', expires_at: futureExpiry(300) })
    const { onOpenChange } = renderDialog()

    await userEvent.type(screen.getByLabelText('Project name'), 'Alpha')
    await userEvent.click(await waitForCreateButton())

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  test('shows the manual fallback and retries with a new permit', async () => {
    const permits: unknown[] = []
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) {
        return ok(
          sessionDto({
            permit_id: 'permit-1',
            state: 'FAILED',
            error: 'ERR_PROJECT_UI_NOT_FOUND',
            updated_at: 2,
          })
        )
      }
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async (_url, data) => {
      permits.push(data)
      const permitId = permits.length === 1 ? 'permit-1' : 'permit-2'
      return ok({ permit_id: permitId, expires_at: futureExpiry(300) })
    }
    renderDialog()

    await userEvent.type(screen.getByLabelText('Project name'), 'Alpha')
    await userEvent.click(await waitForCreateButton())

    expect(
      await screen.findByText('Automatic project creation failed')
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'Create the project manually in the side panel. The remote browser is showing the full window; the system registers it automatically once the guard observes it.'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByText('Error code: ERR_PROJECT_UI_NOT_FOUND')
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => {
      expect(permits).toEqual([{ name: 'Alpha' }, { name: 'Alpha' }])
    })
    expect(
      await screen.findByText('Creating the project in the remote browser...')
    ).toBeInTheDocument()
  })

  test('joins an existing running creation without issuing another permit', async () => {
    let permitCalls = 0
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) {
        return ok(
          sessionDto({
            permit_id: 'permit-running',
            state: 'RUNNING',
            error: '',
            updated_at: 1,
          })
        )
      }
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async () => {
      permitCalls += 1
      return ok({ permit_id: 'permit-new', expires_at: futureExpiry(300) })
    }
    renderDialog()

    expect(
      await screen.findByText('Creating the project in the remote browser...')
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Project name')).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: 'Create project' })
    ).not.toBeInTheDocument()
    expect(permitCalls).toBe(0)
  })

  test('closes after a joined running creation becomes created', async () => {
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) {
        return ok(
          sessionDto({
            permit_id: 'permit-running',
            state: 'RUNNING',
            error: '',
            updated_at: 1,
          })
        )
      }
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async () =>
      ok({ permit_id: 'permit-new', expires_at: futureExpiry(300) })
    const { onOpenChange, queryClient } = renderDialog()

    await screen.findByText('Creating the project in the remote browser...')
    queryClient.setQueryData(
      WEB_WORKSPACE_SESSION_QUERY_KEY,
      sessionDto({
        permit_id: 'permit-running',
        state: 'CREATED',
        error: '',
        updated_at: 2,
      })
    )

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  test('reconciles a creation conflict with the real running session', async () => {
    let sessionCalls = 0
    let permitCalls = 0
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) {
        sessionCalls += 1
        return ok(
          sessionDto(
            sessionCalls === 1
              ? null
              : {
                  permit_id: 'permit-running',
                  state: 'RUNNING',
                  error: '',
                  updated_at: 2,
                }
          )
        )
      }
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async () => {
      permitCalls += 1
      return fail(409, 'WEB_WORKSPACE_PROJECT_CREATION_IN_PROGRESS')
    }
    renderDialog()

    await userEvent.type(screen.getByLabelText('Project name'), 'Alpha')
    await userEvent.click(await waitForCreateButton())

    expect(
      await screen.findByText('Creating the project in the remote browser...')
    ).toBeInTheDocument()
    expect(
      screen.queryByText(
        'A project creation is already running. Finish it or wait for it to fail before starting another.'
      )
    ).not.toBeInTheDocument()
    expect(permitCalls).toBe(1)
  })

  test.each(['FAILED', 'CREATED'] as const)(
    'ignores a stale %s creation state without a matching permit',
    async (state) => {
      let permitPayload: unknown
      apiClient.get = async (url) => {
        if (url === SESSION_PATH) {
          return ok(
            sessionDto({
              permit_id: 'permit-old',
              state,
              error: state === 'FAILED' ? 'ERR_PROJECT_UI_NOT_FOUND' : '',
              updated_at: 1,
            })
          )
        }
        if (url === PERMIT_PATH) return ok({ items: [] })
        throw new Error(`unexpected get ${url}`)
      }
      apiClient.post = async (_url, data) => {
        permitPayload = data
        return ok({ permit_id: 'permit-new', expires_at: futureExpiry(300) })
      }
      renderDialog()

      await userEvent.type(screen.getByLabelText('Project name'), 'Alpha')
      await userEvent.click(await waitForCreateButton())

      expect(
        await screen.findByText('Creating the project in the remote browser...')
      ).toBeInTheDocument()
      expect(permitPayload).toEqual({ name: 'Alpha' })
    }
  )

  test('starts a missing session before retrying the permit', async () => {
    let sessionStarted = false
    let permitCalls = 0
    apiClient.get = async (url) => {
      if (url === SESSION_PATH) return ok(sessionDto(null))
      if (url === PERMIT_PATH) return ok({ items: [] })
      throw new Error(`unexpected get ${url}`)
    }
    apiClient.post = async (url) => {
      if (url === SESSION_PATH) {
        sessionStarted = true
        return ok(sessionDto(null))
      }
      if (!sessionStarted) {
        return fail(409, 'WEB_WORKSPACE_SESSION_REQUIRED')
      }
      permitCalls += 1
      return ok({ permit_id: 'permit-1', expires_at: futureExpiry(300) })
    }
    renderDialog()

    await userEvent.type(screen.getByLabelText('Project name'), 'Alpha')
    await userEvent.click(await waitForCreateButton())
    expect(
      await screen.findByText(
        'Start a browser session before creating a project.'
      )
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Start session' }))
    await userEvent.click(await waitForCreateButton())

    expect(
      await screen.findByText('Creating the project in the remote browser...')
    ).toBeInTheDocument()
    expect(permitCalls).toBe(1)
  })
})
