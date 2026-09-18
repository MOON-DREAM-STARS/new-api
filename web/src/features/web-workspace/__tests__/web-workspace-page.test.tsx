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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { webWorkspaceConfigQueryOptions } from '../hooks/use-web-workspace-config'
import { WebWorkspace } from '../index'

vi.mock('@/components/layout', () => {
  type SlotProps = { children?: React.ReactNode }
  const SectionPageLayout = Object.assign(
    (props: SlotProps) => <div>{props.children}</div>,
    {
      Title: (props: SlotProps) => <h2>{props.children}</h2>,
      Content: (props: SlotProps) => <div>{props.children}</div>,
    }
  )
  return { SectionPageLayout }
})
vi.mock('@novnc/novnc', () => ({
  default: class MockRFB {
    disconnect() {
      /* no stream is attached in these scenarios */
    }
  },
}))

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

const CONFIG_PATH = '/api/web-workspace/config'
const STATUS_PATH = '/api/web-workspace/status'
const SESSION_PATH = '/api/web-workspace/session'
const PROJECTS_PATH = '/api/web-workspace/projects'

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function fail(status: number, code: string, reason?: string) {
  return Promise.reject({
    response: { status, data: { success: false, code, reason } },
  })
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.patch = originalPatch
  apiClient.delete = originalDelete
})

function renderPage(ui: ReactNode) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => ui,
  })
  const dashboardRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/dashboard/$section',
    component: () => null,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, dashboardRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...result, queryClient }
}
describe('WebWorkspace page', () => {
  test('renders the explained disabled page instead of a silent 404', async () => {
    const requested: string[] = []
    apiClient.get = async (url) => {
      requested.push(url)
      if (url === CONFIG_PATH) return ok({ enabled: false, entitled: false })
      if (url === STATUS_PATH) {
        return fail(403, 'WEB_WORKSPACE_ENTITLEMENT_DENIED', 'global_disabled')
      }
      return fail(404, 'WEB_WORKSPACE_RESOURCE_NOT_FOUND')
    }
    const { queryClient } = renderPage(<WebWorkspace />)

    // The route guard primes the capability probe before rendering.
    await queryClient.ensureQueryData(webWorkspaceConfigQueryOptions)

    expect(
      await screen.findByText('Web Workspace is disabled')
    ).toBeInTheDocument()
    expect(
      await screen.findByText('Web Workspace is disabled by the administrator.')
    ).toBeInTheDocument()
    // The layout button renders as an anchor and keeps the button role.
    const back = screen.getByRole('button', { name: 'Back to dashboard' })
    expect(back).toHaveAttribute('href', '/dashboard/overview')
    await waitFor(() => {
      expect(requested).toContain(STATUS_PATH)
    })
    expect(requested).not.toContain(SESSION_PATH)
  })

  test('explains an account-level denial with its reason', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: false })
      if (url === STATUS_PATH) {
        return fail(403, 'WEB_WORKSPACE_ENTITLEMENT_DENIED', 'role')
      }
      return fail(404, 'WEB_WORKSPACE_RESOURCE_NOT_FOUND')
    }
    const { queryClient } = renderPage(<WebWorkspace />)
    await queryClient.ensureQueryData(webWorkspaceConfigQueryOptions)

    expect(
      await screen.findByText('Web Workspace is not available for this account')
    ).toBeInTheDocument()
    expect(
      await screen.findByText(
        'Your account role does not include Web Workspace access.'
      )
    ).toBeInTheDocument()
  })

  test('declares mobile unsupported and disables the remote surface', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(null)
      if (url === PROJECTS_PATH) return ok({ items: [] })
      return fail(404, 'WEB_WORKSPACE_RESOURCE_NOT_FOUND')
    }
    const { queryClient } = renderPage(<WebWorkspace />)
    await queryClient.ensureQueryData(webWorkspaceConfigQueryOptions)

    expect(await screen.findByText('Desktop required')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Web Workspace is not supported on mobile. Use a desktop viewport at least 1024px wide.'
      )
    ).toBeInTheDocument()
    expect(
      await screen.findByText('No browser session is running.')
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'The remote browser is disabled on small screens. Use a desktop viewport at least 1024px wide.'
      )
    ).toBeInTheDocument()
    expect(screen.queryByTestId('web-workspace-surface')).toBeNull()
  })
})
