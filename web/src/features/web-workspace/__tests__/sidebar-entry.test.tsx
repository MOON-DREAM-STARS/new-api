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
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { useSidebarData } from '@/hooks/use-sidebar-data'
import { api } from '@/lib/api'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalAuth = useAuthStore.getState().auth

const CONFIG_PATH = '/api/web-workspace/config'

const USER: AuthUser = {
  id: 1,
  username: 'operator',
  role: 1,
}

function ok(data: unknown) {
  return { data: { success: true, message: '', data } }
}

function setUser(user: AuthUser | null) {
  useAuthStore.setState({
    auth: { ...useAuthStore.getState().auth, user },
  })
}

function NavProbe() {
  const data = useSidebarData()
  const urls = data.navGroups
    .flatMap((group) =>
      group.items.map((item) => ('url' in item && item.url ? item.url : ''))
    )
    .filter(Boolean)
  return <span data-testid='nav-urls'>{urls.join(',')}</span>
}

function renderSidebar() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <NavProbe />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  setUser(USER)
})

afterEach(() => {
  apiClient.get = originalGet
  useAuthStore.setState({ auth: originalAuth })
})

describe('Web Workspace sidebar entry', () => {
  test('shows the entry for an enabled and entitled account', async () => {
    apiClient.get = async (url) => {
      expect(url).toBe(CONFIG_PATH)
      return ok({ enabled: true, entitled: true })
    }
    renderSidebar()

    await waitFor(() => {
      expect(screen.getByTestId('nav-urls').textContent).toContain(
        '/web-workspace'
      )
    })
  })

  test('hides the entry when the feature is disabled', async () => {
    apiClient.get = async () => ok({ enabled: false, entitled: true })
    renderSidebar()

    await waitFor(() => {
      expect(screen.getByTestId('nav-urls').textContent).not.toContain(
        '/web-workspace'
      )
    })
  })

  test('hides the entry when the account is not entitled', async () => {
    apiClient.get = async () => ok({ enabled: true, entitled: false })
    renderSidebar()

    await waitFor(() => {
      expect(screen.getByTestId('nav-urls').textContent).not.toContain(
        '/web-workspace'
      )
    })
  })

  test('does not probe the capability endpoint while signed out', async () => {
    let calls = 0
    setUser(null)
    apiClient.get = async () => {
      calls += 1
      return ok({ enabled: true, entitled: true })
    }
    renderSidebar()

    await waitFor(() => {
      expect(screen.getByTestId('nav-urls').textContent.length).toBeGreaterThan(
        0
      )
    })
    expect(calls).toBe(0)
    expect(screen.getByTestId('nav-urls').textContent).not.toContain(
      '/web-workspace'
    )
  })
})
