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
import { render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode, RefObject } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { RemoteSurfaceController } from '../hooks/use-remote-surface'
import { WEB_WORKSPACE_PROVIDER_QUERY_KEY } from '../hooks/use-web-workspace-provider'
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

const remoteSurfaceState = vi.hoisted(() => ({
  status: 'connected' as
    | 'connecting'
    | 'connected'
    | 'reconnecting'
    | 'failed',
}))

const surfaceOptions = vi.hoisted(() => ({ enabled: true, sessionId: '' }))

vi.mock('../hooks/use-remote-surface', () => ({
  useRemoteSurface: (options: {
    sessionId: string
    enabled: boolean
  }): RemoteSurfaceController => {
    surfaceOptions.enabled = options.enabled
    surfaceOptions.sessionId = options.sessionId
    return {
      containerRef: { current: null } as RefObject<HTMLDivElement | null>,
      screen: { width: 1280, height: 720 },
      status: remoteSurfaceState.status,
      attempt: 0,
      maxAttempts: 5,
      errorMessageKey: null,
      reconnect,
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
  reconnect.mockReset()
  remoteSurfaceState.status = 'connected'
  surfaceOptions.enabled = true
  surfaceOptions.sessionId = ''
  Object.defineProperty(Document.prototype, 'hidden', {
    configurable: true,
    get: () => false,
  })
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
  mode: 'LOCKED',
  created_at: 1,
  last_seen_at: 1,
  idle_deadline_at: 600,
  stream_bytes_out: 2020000,
  stream_bytes_in: 1000000,
  navigation: {
    can_go_back: true,
    can_go_forward: true,
    updated_at: 1,
  },
}

function mockWorkspaceGets(session: unknown) {
  apiClient.get = async (url) => {
    if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
    if (url === SESSION_PATH) return ok(session)
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

  test('shows remote page retry health with the real attempts and error code', async () => {
    mockWorkspaceGets({
      ...runningSession,
      page: {
        state: 'RETRYING',
        error: 'ERR_TUNNEL_CONNECTION_FAILED',
        attempts: 2,
        updated_at: 2,
      },
    })

    renderPage(<WebWorkspace />)

    expect(
      await screen.findByText('The remote page failed to load. Retrying...')
    ).toBeTruthy()
    expect(
      screen.getByText('Retried 2 times · ERR_TUNNEL_CONNECTION_FAILED')
    ).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Reload' })).toBeNull()
  })

  test('reloads a failed remote page through the navigation API', async () => {
    const failedSession = {
      ...runningSession,
      page: {
        state: 'FAILED',
        error: 'ERR_TUNNEL_CONNECTION_FAILED',
        attempts: 3,
        updated_at: 3,
      },
    }
    mockWorkspaceGets(failedSession)

    const posts: Array<{ url: string; data: unknown }> = []
    let resolvePost: ((value: { data: unknown }) => void) | undefined
    apiClient.post = async (url, data) => {
      posts.push({ url, data })
      return new Promise<{ data: unknown }>((resolve) => {
        resolvePost = resolve
      })
    }

    renderPage(<WebWorkspace />)

    const reloadButton = await screen.findByRole('button', { name: 'Reload' })
    reloadButton.click()

    await waitFor(() => {
      expect(posts).toEqual([
        {
          url: `${SESSION_PATH}/${runningSession.session_id}/navigation`,
          data: { action: 'reload' },
        },
      ])
      expect((reloadButton as HTMLButtonElement).disabled).toBe(true)
    })

    resolvePost?.(
      ok({
        ...failedSession,
        page: {
          state: 'READY',
          error: '',
          attempts: 0,
          updated_at: 4,
        },
      })
    )
  })

  test('does not render page health when the remote page is READY', async () => {
    mockWorkspaceGets({
      ...runningSession,
      page: {
        state: 'READY',
        error: '',
        attempts: 0,
        updated_at: 4,
      },
    })

    renderPage(<WebWorkspace />)

    await screen.findByRole('button', { name: 'Browser back' })
    expect(
      screen.queryByText('The remote page failed to load. Retrying...')
    ).toBeNull()
    expect(screen.queryByText('The remote page failed to load')).toBeNull()
  })

  test('does not render page health when page is null', async () => {
    mockWorkspaceGets({ ...runningSession, page: null })

    renderPage(<WebWorkspace />)

    await screen.findByRole('button', { name: 'Browser back' })
    expect(
      screen.queryByText('The remote page failed to load. Retrying...')
    ).toBeNull()
    expect(screen.queryByText('The remote page failed to load')).toBeNull()
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

  test('detaches the display stream while the tab is hidden', async () => {
    mockWorkspaceGets(runningSession)

    renderPage(<WebWorkspace />)

    await waitFor(() => {
      expect(surfaceOptions.sessionId).toBe(runningSession.session_id)
      expect(surfaceOptions.enabled).toBe(true)
    })

    Object.defineProperty(Document.prototype, 'hidden', {
      configurable: true,
      get: () => true,
    })
    document.dispatchEvent(new Event('visibilitychange'))

    await waitFor(() => {
      expect(surfaceOptions.enabled).toBe(false)
    })
  })
  test('opens the overflow menu and locks a signed-in runtime', async () => {
    mockWorkspaceGets({ ...runningSession, mode: 'LOGIN' })
    const restartPosts: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      if (url === `${SESSION_PATH}/${runningSession.session_id}/restart`) {
        restartPosts.push({ url, data })
        return ok({ ...runningSession, mode: 'LOCKED' })
      }
      throw new Error(`unexpected post ${url}`)
    }

    renderPage(<WebWorkspace />)

    const menuButton = await screen.findByRole('button', {
      name: 'More workspace actions',
    })
    menuButton.click()

    const lockItem = await screen.findByRole('menuitem', {
      name: 'Finish sign-in and lock',
    })
    lockItem.click()

    await waitFor(() => {
      expect(restartPosts[0]?.url).toBe(
        `${SESSION_PATH}/${runningSession.session_id}/restart`
      )
      const posted = restartPosts[0]
      expect(posted).toBeDefined()
      if (!posted) return
      expect((posted.data as { mode?: string }).mode).toBe('LOCKED')
    })
  })
  test('proposes the measured remote screen size when starting', async () => {
    const posts: Array<{ url: string; data: unknown }> = []
    apiClient.get = async (url) => {
      if (url === CONFIG_PATH) return ok({ enabled: true, entitled: true })
      if (url === SESSION_PATH) return ok(null)
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
    apiClient.post = async (url, data) => {
      posts.push({ url, data })
      return ok(runningSession)
    }

    const view = renderPage(<WebWorkspace />)
    // The provider query decides which calibrated profile the proposal uses.
    view.queryClient.setQueryData(WEB_WORKSPACE_PROVIDER_QUERY_KEY, 'chatgpt')

    const startButtons = await screen.findAllByText('Start session')
    startButtons[0].click()

    await waitFor(() => {
      expect(posts[0]).toEqual({
        url: SESSION_PATH,
        data: { screen_width: 1860, screen_height: 860 },
      })
    })
  })
  test('sends the selected navigation command through the real API', async () => {
    mockWorkspaceGets(runningSession)
    const navigationPosts: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      navigationPosts.push({ url, data })
      return ok({
        ...runningSession,
        navigation: {
          can_go_back: false,
          can_go_forward: false,
          updated_at: 2,
        },
      })
    }

    renderPage(<WebWorkspace />)

    const backButton = await screen.findByRole('button', {
      name: 'Browser back',
    })
    const forwardButton = screen.getByRole('button', {
      name: 'Browser forward',
    })
    const reloadButton = screen.getByRole('button', {
      name: 'Refresh page',
    })

    await waitFor(() => {
      expect((backButton as HTMLButtonElement).disabled).toBe(false)
      expect((forwardButton as HTMLButtonElement).disabled).toBe(false)
      expect((reloadButton as HTMLButtonElement).disabled).toBe(false)
    })

    backButton.click()
    await waitFor(() => {
      expect(navigationPosts[0]).toEqual({
        url: `${SESSION_PATH}/${runningSession.session_id}/navigation`,
        data: { action: 'back' },
      })
      expect((forwardButton as HTMLButtonElement).disabled).toBe(false)
    })

    forwardButton.click()
    await waitFor(() => {
      expect(navigationPosts[1]?.data).toEqual({ action: 'forward' })
      expect((reloadButton as HTMLButtonElement).disabled).toBe(false)
    })

    reloadButton.click()
    await waitFor(() => {
      expect(navigationPosts[2]?.data).toEqual({ action: 'reload' })
    })
  })

  test('marks the operator-opened sign-in window', async () => {
    mockWorkspaceGets({ ...runningSession, mode: 'LOGIN' })

    renderPage(<WebWorkspace />)

    expect(await screen.findByText('Login mode')).toBeTruthy()
  })

  test('does not mark a locked session', async () => {
    mockWorkspaceGets(runningSession)

    renderPage(<WebWorkspace />)

    await screen.findByRole('button', { name: 'Browser back' })
    expect(screen.queryByText('Login mode')).toBeNull()
  })
  test('disables navigation when the session is not live', async () => {
    mockWorkspaceGets({ ...runningSession, state: 'STOPPED' })

    renderPage(<WebWorkspace />)

    const backButton = await screen.findByRole('button', {
      name: 'Browser back',
    })
    expect((backButton as HTMLButtonElement).disabled).toBe(true)
    expect(
      (screen.getByRole('button', {
        name: 'Browser forward',
      }) as HTMLButtonElement).disabled
    ).toBe(true)
    expect(
      (screen.getByRole('button', {
        name: 'Refresh page',
      }) as HTMLButtonElement).disabled
    ).toBe(true)
  })

  test('disables navigation while the remote surface is not connected', async () => {
    remoteSurfaceState.status = 'connecting'
    mockWorkspaceGets(runningSession)

    renderPage(<WebWorkspace />)

    const backButton = await screen.findByRole('button', {
      name: 'Browser back',
    })
    expect((backButton as HTMLButtonElement).disabled).toBe(true)
    expect(
      (screen.getByRole('button', {
        name: 'Browser forward',
      }) as HTMLButtonElement).disabled
    ).toBe(true)
    expect(
      (screen.getByRole('button', {
        name: 'Refresh page',
      }) as HTMLButtonElement).disabled
    ).toBe(true)
  })

  test('keeps navigation and an exit control in immersive mode', async () => {
    mockWorkspaceGets(runningSession)

    renderPage(<WebWorkspace />)

    const immersiveButton = await screen.findByRole('button', {
      name: 'Immersive mode',
    })
    immersiveButton.click()

    const immersiveControls = await screen.findByTestId(
      'web-workspace-immersive-controls'
    )
    expect(
      within(immersiveControls).getByRole('button', { name: 'Browser back' })
    ).toBeTruthy()
    expect(
      within(immersiveControls).getByRole('button', {
        name: 'Browser forward',
      })
    ).toBeTruthy()
    expect(
      within(immersiveControls).getByRole('button', { name: 'Refresh page' })
    ).toBeTruthy()
    expect(
      within(immersiveControls).getByRole('button', {
        name: 'Exit immersive mode',
      })
    ).toBeTruthy()
  })
})
