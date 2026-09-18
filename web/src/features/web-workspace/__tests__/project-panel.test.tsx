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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { ProjectPanel } from '../components/project-panel'
import { WEB_WORKSPACE_PROJECTS_QUERY_KEY } from '../constants'
import type { WebProject } from '../types'

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
  patch: ApiMethod
  delete: ApiMethod
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalPatch = apiClient.patch
const originalDelete = apiClient.delete

const PROJECTS_PATH = '/api/web-workspace/projects'

const ALPHA: WebProject = {
  id: 1,
  provider: 'chatgpt',
  name: 'Alpha',
  created_at: 1_700_000_000,
  updated_at: 1_700_000_000,
}

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function fail(status: number, code: string) {
  return Promise.reject({
    response: { status, data: { success: false, code } },
  })
}

function unexpected(url: string): never {
  throw new Error(['Unexpected request', url].join(' '))
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.patch = originalPatch
  apiClient.delete = originalDelete
})

function installProjectList(projects: WebProject[]) {
  apiClient.get = async (url) => {
    if (url !== PROJECTS_PATH) return unexpected(url)
    return ok({ items: [...projects] })
  }
}

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <ProjectPanel />
    </QueryClientProvider>
  )
  return { ...result, queryClient }
}

describe('ProjectPanel', () => {
  test('renders registered projects with accessible rename and delete actions', async () => {
    installProjectList([ALPHA])
    renderPanel()

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('chatgpt')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Rename Alpha' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Delete Alpha' })
    ).toBeInTheDocument()
  })

  test('shows the empty state with a creation entry', async () => {
    installProjectList([])
    renderPanel()

    expect(await screen.findByText('No projects yet.')).toBeInTheDocument()
    expect(
      screen.getAllByRole('button', { name: 'New project' }).length
    ).toBeGreaterThan(0)
  })

  test('shows a retry entry when the project request fails', async () => {
    let attempts = 0
    apiClient.get = async (url) => {
      if (url !== PROJECTS_PATH) return unexpected(url)
      attempts += 1
      if (attempts === 1) return fail(503, 'WEB_WORKSPACE_AGENT_UNAVAILABLE')
      return ok({ items: [ALPHA] })
    }
    renderPanel()

    const alert = await screen.findByRole('alert')
    expect(
      within(alert).getByText('Could not load projects')
    ).toBeInTheDocument()
    await userEvent.click(within(alert).getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
  })

  test('refreshes the list through the server-side sync', async () => {
    const projects: WebProject[] = []
    let getCalls = 0
    apiClient.get = async (url) => {
      if (url !== PROJECTS_PATH) return unexpected(url)
      getCalls += 1
      return ok({ items: [...projects] })
    }
    renderPanel()

    expect(await screen.findByText('No projects yet.')).toBeInTheDocument()
    projects.push(ALPHA)
    await userEvent.click(
      screen.getByRole('button', { name: 'Refresh projects' })
    )

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(getCalls).toBe(2)
  })

  test('renames a project with an optimistic list update', async () => {
    const projects = [ALPHA]
    installProjectList(projects)
    apiClient.patch = async (url, data) => {
      expect(url).toBe('/api/web-workspace/projects/1')
      expect(data).toEqual({ name: 'Beta' })
      projects[0] = { ...projects[0], name: 'Beta' }
      return ok(projects[0])
    }
    renderPanel()

    await screen.findByText('Alpha')
    await userEvent.click(screen.getByRole('button', { name: 'Rename Alpha' }))
    const input = screen.getByLabelText('Project name')
    expect(input).toHaveFocus()
    await userEvent.clear(input)
    await userEvent.type(input, 'Beta')
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    expect(await screen.findByText('Beta')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
  })

  test('rolls the renamed project back when the request fails', async () => {
    installProjectList([ALPHA])
    apiClient.patch = async () => fail(500, 'WEB_WORKSPACE_INTERNAL_ERROR')
    renderPanel()

    await screen.findByText('Alpha')
    await userEvent.click(screen.getByRole('button', { name: 'Rename Alpha' }))
    const input = screen.getByLabelText('Project name')
    await userEvent.clear(input)
    await userEvent.type(input, 'Beta')
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.queryByText('Beta')).toBeNull()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Something went wrong. Please try again.'
    )
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  test('closes the rename dialog with Escape', async () => {
    installProjectList([ALPHA])
    renderPanel()

    await screen.findByText('Alpha')
    await userEvent.click(screen.getByRole('button', { name: 'Rename Alpha' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
  })

  test('deletes a project after a keyboard-confirmed dialog', async () => {
    const projects = [ALPHA]
    installProjectList(projects)
    apiClient.delete = async (url) => {
      expect(url).toBe('/api/web-workspace/projects/1')
      projects.length = 0
      return ok({ id: 1 })
    }
    renderPanel()

    await screen.findByText('Alpha')
    await userEvent.click(screen.getByRole('button', { name: 'Delete Alpha' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(
      within(dialog).getByText('Delete project Alpha?')
    ).toBeInTheDocument()

    const confirm = within(dialog).getByRole('button', {
      name: 'Delete project',
    })
    confirm.focus()
    expect(confirm).toHaveFocus()
    await userEvent.keyboard('{Enter}')

    await waitFor(() => {
      expect(screen.queryByText('Alpha')).toBeNull()
    })
    expect(screen.queryByText('Resource unavailable or removed')).toBeNull()
  })

  test('explains a project removed by the guard and offers re-registration', async () => {
    installProjectList([ALPHA])
    const { queryClient } = renderPanel()

    await screen.findByText('Alpha')
    act(() => {
      queryClient.setQueryData(WEB_WORKSPACE_PROJECTS_QUERY_KEY, [])
    })

    expect(
      await screen.findByText('Resource unavailable or removed')
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Start registration flow' })
    ).toBeInTheDocument()
    expect(
      screen.getByText(/Unregistered projects are rejected/)
    ).toBeInTheDocument()
  })
})
