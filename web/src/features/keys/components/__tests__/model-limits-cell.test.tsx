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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import type { ApiKey } from '../../types'

const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ModelLimitsCell } = await import('../api-keys-cells')

type MockableApi = { get: (url: string) => Promise<{ data: unknown }> }
const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get

const families = {
  success: true,
  data: {
    families: [
      { id: 1, key: 'claude', name: 'Claude', description: '', models: [] },
      { id: 2, key: 'gpt', name: 'GPT', description: '', models: [] },
    ],
    unclassified_models: [],
  },
}

function apiKey(patch: Partial<ApiKey>): ApiKey {
  return {
    allow_user_quota_query: false,
    id: 1,
    name: 'key',
    key: 'abcd',
    status: 1,
    remain_quota: 0,
    used_quota: 0,
    unlimited_quota: true,
    expired_time: -1,
    created_time: 1,
    accessed_time: 1,
    group: '',
    auto_groups: null,
    cross_group_retry: false,
    model_limits_enabled: false,
    model_limits: '',
    model_families: [],
    allow_ips: '',
    ...patch,
  }
}

function renderCell(key: ApiKey) {
  const requested: string[] = []
  apiClient.get = async (url) => {
    requested.push(url)
    if (url === '/api/user/model_families') return { data: families }
    throw new Error(`Unexpected GET ${url}`)
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ModelLimitsCell apiKey={key} detailsTrigger='click' />
    </QueryClientProvider>
  )
  return requested
}

afterEach(() => {
  apiClient.get = originalGet
})

describe('model limits cell', () => {
  test('shows no limit for a key without families or whitelisted models', () => {
    const requested = renderCell(apiKey({}))

    expect(screen.getByText('Unlimited')).toBeVisible()
    expect(requested).toEqual([])
  })

  test('names the families a key is limited to', async () => {
    renderCell(apiKey({ model_families: [1, 2] }))

    await userEvent.click(
      screen.getByRole('button', { name: /2 model families/ })
    )

    expect(await screen.findByText('Claude')).toBeVisible()
    expect(screen.getByText('GPT')).toBeVisible()
  })

  test('shows the families together with whitelisted models', async () => {
    renderCell(
      apiKey({
        model_families: [2],
        model_limits_enabled: true,
        model_limits: 'gpt-4o,gpt-5',
      })
    )

    const trigger = screen.getByRole('button', {
      name: /1 model families · 2 models/,
    })
    await userEvent.click(trigger)

    expect(await screen.findByText('GPT')).toBeVisible()
    expect(screen.getByText('gpt-5')).toBeVisible()
  })
})
