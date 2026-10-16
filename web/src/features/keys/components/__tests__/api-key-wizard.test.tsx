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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { ApiKey } from '../../types'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { ApiKeysProvider } = await import('../api-keys-provider')
const { ApiKeyWizardDialog } =
  await import('../api-key-wizard/api-key-wizard-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type Payload = Record<string, unknown>

// What goes over the wire: JSON drops fields whose value is undefined.
function omitUndefined(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(omitUndefined)
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, item]) => item !== undefined)
        .map(([key, item]) => [key, omitUndefined(item)])
    )
  }
  return value
}

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod; post: ApiMethod; put: ApiMethod }

const apiClient = api as unknown as MockableApi
const original = {
  get: apiClient.get,
  post: apiClient.post,
  put: apiClient.put,
}

const families = {
  success: true,
  data: {
    families: [
      {
        id: 1,
        key: 'claude',
        name: 'Claude',
        description: 'Anthropic',
        models: [],
      },
      {
        id: 2,
        key: 'gpt',
        name: 'GPT',
        description: 'OpenAI',
        models: ['gpt-5'],
      },
      {
        id: 4,
        key: 'deepseek',
        name: 'DeepSeek',
        description: 'DeepSeek AI',
        models: ['deepseek-v4-pro'],
      },
    ],
    unclassified_models: ['custom-model'],
  },
}
const accountTiers = {
  enabled_tiers: ['economy', 'balanced', 'performance'],
  preference: 'lowest',
  allow_escalation: false,
  max_escalation_tier: '',
  version: 3,
}
const tierOverview = {
  success: true,
  data: {
    available: true,
    settings: { ...accountTiers, is_default: true },
    preview: {
      chain: [],
      families: [
        {
          family: 'DeepSeek',
          offered_tiers: { economy: 0.15, balanced: 0.2, performance: 0.4 },
          model_overrides: 0,
          preferred_tier: 'economy',
          preferred_offered: true,
          steps: [],
          ceiling_ratio: 0.4,
        },
      ],
    },
  },
}
const storedKey = {
  id: 5,
  name: 'stored',
  key: 'abcd',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 1,
  group: 'vip',
  auto_groups: ['vip', 'default'],
  cross_group_retry: true,
  model_limits_enabled: true,
  model_limits: 'gpt-4o',
  allow_ips: '',
  model_families: [2],
  service_tier: {
    ...accountTiers,
    enabled_tiers: ['performance'],
    preference: 'performance',
    version: 8,
  },
}

type Calls = { created: Payload[]; updated: Payload[]; revealed: number[][] }

function installApi(options: { failSecondCreate?: boolean } = {}): Calls {
  const calls: Calls = { created: [], updated: [], revealed: [] }
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/status':
        return { data: { data: { default_use_auto_group: true } } }
      case '/api/user/self/groups':
        return {
          data: {
            success: true,
            data: {
              auto: { desc: 'Auto', ratio: 'auto' },
              default: { desc: 'Default', ratio: 1 },
            },
          },
        }
      case '/api/user/model_families':
        return { data: families }
      case '/api/user/service_tier':
        return { data: tierOverview }
      case '/api/token/5':
        return { data: { success: true, data: storedKey } }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async (url, data) => {
    if (url === '/api/token/') {
      const payload = omitUndefined(data) as Payload
      calls.created.push(payload)
      if (options.failSecondCreate && calls.created.length === 2) {
        return { data: { success: false, message: 'Token limit reached' } }
      }
      const id = 100 + calls.created.length
      return { data: { success: true, data: { id, name: payload.name } } }
    }
    if (url === '/api/token/batch/keys') {
      const ids = (data as { ids: number[] }).ids
      calls.revealed.push(ids)
      return {
        data: {
          success: true,
          data: {
            keys: Object.fromEntries(ids.map((id) => [id, `real${id}`])),
          },
        },
      }
    }
    throw new Error(`Unexpected POST ${url}`)
  }
  apiClient.put = async (url, data) => {
    expect(url).toBe('/api/token/')
    calls.updated.push(omitUndefined(data) as Payload)
    return { data: { success: true, data: {} } }
  }
  return calls
}

let queryClient: InstanceType<typeof QueryClient> | null = null

function renderWizard(currentRow?: ApiKey) {
  const onOpenChange = vi.fn()
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(
    ['status'],
    { default_use_auto_group: true },
    { updatedAt: Date.now() + 60_000 }
  )
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ApiKeysProvider>
          <ApiKeyWizardDialog
            open
            onOpenChange={onOpenChange}
            currentRow={currentRow}
          />
        </ApiKeysProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  return { onOpenChange }
}

afterEach(() => {
  Object.assign(apiClient, original)
  queryClient?.clear()
  queryClient = null
})

const next = () => userEvent.click(screen.getByRole('button', { name: 'Next' }))
const familyBox = (name: string) => screen.findByRole('checkbox', { name })

describe('API key wizard: creating keys', () => {
  test('selects every model family by default and creates a key without a family limit', async () => {
    const calls = installApi()
    const { onOpenChange } = renderWizard()

    for (const name of ['Claude', 'GPT', 'DeepSeek', 'Other models']) {
      expect(await familyBox(name)).toBeChecked()
    }
    expect(screen.getByText('4 of 4 model families selected')).toBeVisible()

    await next()
    expect(
      await screen.findByText('Following your account default')
    ).toBeVisible()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'my-key')
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    await waitFor(() => expect(calls.created).toHaveLength(1))
    expect(calls.created[0]).toMatchObject({
      name: 'my-key',
      group: 'auto',
      cross_group_retry: true,
      model_limits_enabled: false,
    })
    for (const absent of ['model_families', 'service_tier', 'auto_groups']) {
      expect(calls.created[0]).not.toHaveProperty(absent)
    }

    const warning = await screen.findByRole('alert')
    expect(
      within(warning).getByText('Never share or leak this API Key!')
    ).toBeVisible()
    expect(warning).toHaveClass('text-destructive')
    expect(await screen.findByText('sk-real101')).toBeVisible()
    expect(calls.revealed).toEqual([[101]])
    expect(screen.queryByRole('button', { name: 'Back' })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  test('limits the key to the families that stay selected', async () => {
    const calls = installApi()
    renderWizard()

    await userEvent.click(await familyBox('Claude'))
    expect(screen.getByText('3 of 4 model families selected')).toBeVisible()
    await next()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'limited')
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    await waitFor(() => expect(calls.created).toHaveLength(1))
    expect(calls.created[0]?.model_families).toEqual([0, 2, 4])
  })

  test('does not move on while no model family is selected', async () => {
    installApi()
    renderWizard()

    await familyBox('GPT')
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }))

    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
    expect(screen.getByText('Select at least one model family')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Select all' }))
    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  test('opens every page at its top', async () => {
    installApi()
    const scrollIntoView = vi.spyOn(HTMLElement.prototype, 'scrollIntoView')
    try {
      renderWizard()
      await familyBox('GPT')
      scrollIntoView.mockClear()

      await next()
      expect(scrollIntoView).toHaveBeenCalledTimes(1)
      await userEvent.click(screen.getByRole('button', { name: 'Back' }))
      expect(scrollIntoView).toHaveBeenCalledTimes(2)
    } finally {
      scrollIntoView.mockRestore()
    }
  })

  test('stores tier settings on the key only when they were changed', async () => {
    const calls = installApi()
    renderWizard()

    await familyBox('GPT')
    await next()
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Economy tier' })
    )
    expect(screen.getByText('Custom tiers for this API key')).toBeVisible()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'tiered')
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    await waitFor(() => expect(calls.created).toHaveLength(1))
    expect(calls.created[0]?.service_tier).toEqual({
      enabled_tiers: ['balanced', 'performance'],
      preference: 'lowest',
      allow_escalation: false,
      max_escalation_tier: '',
    })
  })

  test('sends no tier settings after the key is set back to follow the account', async () => {
    const calls = installApi()
    renderWizard()

    await familyBox('GPT')
    await next()
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Economy tier' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Follow account default' })
    )
    expect(screen.getByText('Following your account default')).toBeVisible()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'plain')
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    await waitFor(() => expect(calls.created).toHaveLength(1))
    expect(calls.created[0]).not.toHaveProperty('service_tier')
  })

  test('shows every key of a batch with a button that copies them all', async () => {
    const calls = installApi()
    renderWizard()

    await familyBox('GPT')
    await next()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'batch')
    fireEvent.input(screen.getByLabelText('Quantity'), {
      target: { value: '2' },
    })
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    expect(await screen.findByText('sk-real101')).toBeVisible()
    expect(screen.getByText('sk-real102')).toBeVisible()
    expect(calls.created.map((payload) => payload.name)).toEqual([
      'batch',
      expect.stringMatching(/^batch-/),
    ])
    expect(calls.revealed).toEqual([[101, 102]])
    expect(
      screen.getByRole('button', { name: 'Copy all API keys' })
    ).toBeVisible()
  })

  test('keeps the keys that were created when a later one fails', async () => {
    const calls = installApi({ failSecondCreate: true })
    renderWizard()

    await familyBox('GPT')
    await next()
    await next()
    await userEvent.type(screen.getByLabelText('Name'), 'partial')
    fireEvent.input(screen.getByLabelText('Quantity'), {
      target: { value: '3' },
    })
    await userEvent.click(
      screen.getByRole('button', { name: 'Create API Key' })
    )

    expect(await screen.findByText('sk-real101')).toBeVisible()
    expect(
      screen.getByText(
        'Only 1 of 3 API keys were created. Create the rest again to get them.'
      )
    ).toBeVisible()
    expect(calls.created).toHaveLength(2)
  })
})

describe('API key wizard: editing a key', () => {
  const openStep = (name: RegExp) =>
    userEvent.click(screen.getByRole('button', { name }))
  const save = () =>
    userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  test('shows the stored family limit and keeps every stored setting when saved untouched', async () => {
    const calls = installApi()
    const { onOpenChange } = renderWizard(storedKey as ApiKey)

    expect(await familyBox('GPT')).toBeChecked()
    for (const name of ['Claude', 'DeepSeek', 'Other models']) {
      expect(await familyBox(name)).not.toBeChecked()
    }
    await openStep(/Settings/)
    expect(await screen.findByLabelText('Name')).toHaveValue('stored')
    expect(screen.queryByLabelText('Quantity')).toBeNull()
    await save()

    await waitFor(() => expect(calls.updated).toHaveLength(1))
    expect(calls.updated[0]).toMatchObject({
      id: 5,
      name: 'stored',
      group: 'vip',
      cross_group_retry: true,
      model_limits_enabled: true,
      model_limits: 'gpt-4o',
    })
    for (const absent of ['model_families', 'service_tier', 'auto_groups']) {
      expect(calls.updated[0]).not.toHaveProperty(absent)
    }
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  test("shows the key's own tiers and sends null when it follows the account again", async () => {
    const calls = installApi()
    renderWizard(storedKey as ApiKey)

    await familyBox('GPT')
    await openStep(/Service tier/)
    expect(
      await screen.findByText('Custom tiers for this API key')
    ).toBeVisible()
    expect(
      screen.getByRole('switch', { name: 'Economy tier' })
    ).not.toBeChecked()
    await userEvent.click(
      screen.getByRole('button', { name: 'Follow account default' })
    )
    expect(screen.getByText('Following your account default')).toBeVisible()
    expect(screen.getByRole('switch', { name: 'Economy tier' })).toBeChecked()
    await openStep(/Settings/)
    await save()

    await waitFor(() => expect(calls.updated).toHaveLength(1))
    expect(calls.updated[0]).toHaveProperty('service_tier', null)
  })

  test('sends the new family limit when the selection changes', async () => {
    const calls = installApi()
    renderWizard(storedKey as ApiKey)

    await userEvent.click(await familyBox('Claude'))
    await openStep(/Settings/)
    await save()

    await waitFor(() => expect(calls.updated).toHaveLength(1))
    expect(calls.updated[0]?.model_families).toEqual([1, 2])
  })

  test('removes the family limit when everything is selected again', async () => {
    const calls = installApi()
    renderWizard(storedKey as ApiKey)

    await familyBox('GPT')
    await userEvent.click(screen.getByRole('button', { name: 'Select all' }))
    await openStep(/Settings/)
    await save()

    await waitFor(() => expect(calls.updated).toHaveLength(1))
    expect(calls.updated[0]?.model_families).toEqual([])
  })

  test('cannot open later pages while no family is selected', async () => {
    installApi()
    renderWizard(storedKey as ApiKey)

    await familyBox('GPT')
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }))

    expect(screen.getByRole('button', { name: /Settings/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Service tier/ })).toBeDisabled()
  })
})
