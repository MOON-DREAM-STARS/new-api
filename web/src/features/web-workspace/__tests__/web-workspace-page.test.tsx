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
import type { ReactNode, RefObject } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { RemoteSurfaceController } from '../hooks/use-remote-surface'
import { WebWorkspace } from '../index'

vi.mock('@/components/layout', () => {
  type SlotProps = { children?: React.ReactNode }
  const SectionPageLayout = Object.assign(
    (props: SlotProps) => <div>{props.children}</div>,
    {
      Title: (props: SlotProps) => <h2>{props.children}</h2>,
      Actions: (props: SlotProps) => <div>{props.children}</div>,
      Content: (props: SlotProps) => <div>{props.children}</div>,
    }
  )
  return { SectionPageLayout }
})

vi.mock('@/components/ui/sidebar', () => ({
  useSidebar: () => ({ open: true, setOpen: () => undefined }),
}))

vi.mock('@/context/layout-provider', () => ({
  useLayout: () => ({ collapsible: 'icon', setCollapsible: () => undefined }),
}))

vi.mock('../hooks/use-element-size', () => ({
  useElementSize: () => ({ width: 1600, height: 860 }),
}))

vi.mock('../hooks/use-desktop-viewport', () => ({
  useDesktopViewport: () => true,
}))

const reconnect = vi.fn()

vi.mock('../hooks/use-remote-surface', () => ({
  useRemoteSurface: (): RemoteSurfaceController => ({
    containerRef: { current: null } as RefObject<HTMLDivElement | null>,
    screen: { width: 1280, height: 720 },
    status: 'connected',
    attempt: 0,
    maxAttempts: 5,
    errorMessageKey: null,
    reconnect,
  }),
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
  reconnect.mockReset()
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

const runningSession = {
  session_id: 'session-1',
  state: 'RUNNING',
  created_at: 1,
  last_seen_at: 1,
  idle_deadline_at: 600,
}

describe('WebWorkspace page', () => {
  test('renders the explained disabled page instead of a silent 404', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: false, entitled: false })
      if (url === STATUS_PATH) {
        return fail(403, 'WEB_WORKSPACE_ENTITLEMENT_DENIED', 'global_disabled')
      }
      throw new Error(`unexpected request ${url}`)
    }

    renderPage(<WebWorkspace />)

    expect(
      await screen.findByText('Web Workspace is disabled by the administrator.')
    ).toBeTruthy()
  })

  test('renders real projects, the compact rail and the real session state', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(runningSession)
      if (url === PROJECTS_PATH) {
        return ok({
          items: [
            {
              id: 1,
              provider: 'chatgpt',
              name: 'acceptance project',
              created_at: 1,
              updated_at: 1,
            },
          ],
        })
      }
      if (url === STATUS_PATH) {
        return ok({
          entitled: true,
          workspace: {
            provider: 'chatgpt',
            status: 1,
            created_at: 1,
            last_active_at: 1,
          },
        })
      }
      throw new Error(`unexpected request ${url}`)
    }

    renderPage(<WebWorkspace />)

    await waitFor(() => {
      expect(screen.getByText('Connected')).toBeTruthy()
    })
    expect(screen.getByLabelText('acceptance project')).toBeTruthy()
    expect(screen.getByTestId('web-workspace-surface')).toBeTruthy()
    expect(screen.getByTestId('web-workspace-surface-frame')).toBeTruthy()
    // The remote browser stays the primary surface: no Card based dashboard.
    expect(screen.queryByText('Browser session')).toBeNull()
  })

  test('never invents a project when the account has none', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(runningSession)
      if (url === PROJECTS_PATH) return ok({ items: [] })
      if (url === STATUS_PATH) {
        return ok({
          entitled: true,
          workspace: {
            provider: 'chatgpt',
            status: 1,
            created_at: 1,
            last_active_at: 1,
          },
        })
      }
      throw new Error(`unexpected request ${url}`)
    }

    const { container } = renderPage(<WebWorkspace />)

    await waitFor(() => {
      expect(screen.getByTestId('web-workspace-surface-frame')).toBeTruthy()
    })
    expect(screen.queryByLabelText('A0')).toBeNull()
    expect(screen.queryByLabelText('A1')).toBeNull()
    expect(screen.queryByLabelText('A2')).toBeNull()
    expect(container.textContent).not.toContain('A0')
  })

  test('shows the real empty state for an account without projects', async () => {
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(null)
      if (url === PROJECTS_PATH) return ok({ items: [] })
      if (url === STATUS_PATH) {
        return ok({ entitled: true, workspace: undefined })
      }
      throw new Error(`unexpected request ${url}`)
    }

    const { container } = renderPage(<WebWorkspace />)

    expect(await screen.findByText('No projects yet.')).toBeTruthy()
    expect(container.textContent).not.toContain('A0')
    expect(screen.queryByLabelText('A0')).toBeNull()
  })

  test('shows the workspace empty state and starts a session on demand', async () => {
    const posted: string[] = []
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(null)
      if (url === PROJECTS_PATH) {
        return ok({
          items: [
            {
              id: 1,
              provider: 'chatgpt',
              name: 'acceptance project',
              created_at: 1,
              updated_at: 1,
            },
          ],
        })
      }
      if (url === STATUS_PATH) {
        return ok({ entitled: true, workspace: undefined })
      }
      throw new Error(`unexpected request ${url}`)
    }
    apiClient.post = async (url) => {
      posted.push(url)
      return ok(runningSession)
    }

    renderPage(<WebWorkspace />)

    expect(
      await screen.findByText('Remote browser is not running')
    ).toBeTruthy()
    const startButtons = await screen.findAllByText('Start session')
    startButtons[0].click()
    await waitFor(() => {
      expect(posted).toContain(SESSION_PATH)
    })
  })
})
