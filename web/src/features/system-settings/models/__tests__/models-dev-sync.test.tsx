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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type {
  ModelsDevCatalogItem,
  ModelsDevConfig,
  ModelsDevPlan,
  ModelsDevRow,
  ModelsDevStateView,
} from '../models-dev-sync/types'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const lib = await import('../models-dev-sync/lib')
const { ModelsDevSync } = await import('../models-dev-sync/models-dev-sync')
const { ImportFromModelsDev } =
  await import('../models-dev-sync/import-from-models-dev')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

// The console names Simplified Chinese "zhCN", which Intl does not accept.
const chinese = createInstance()
await chinese.use(initReactI18next).init({
  lng: 'zhCN',
  resources: { zhCN: { translation: {} } },
})

const BASE = '/api/option/models_dev'
const noPrice = {
  priced: false,
  quota_type: 0,
  model_ratio: 0,
  model_price: 0,
  completion_ratio: 0,
}
const summary = {
  matched: 0,
  created: 0,
  updated: 0,
  unchanged: 0,
  locked: 0,
  skipped_review: 0,
  invalid: 0,
  unmatched: 0,
}

function config(patch: Partial<ModelsDevConfig> = {}): ModelsDevConfig {
  return {
    auto_sync_enabled: false,
    include_site_models: true,
    include_common_models: true,
    selected_keys: [],
    excluded_keys: [],
    mappings: {},
    locked_models: [],
    unlocked_models: [],
    ...patch,
  }
}

function row(patch: Partial<ModelsDevRow>): ModelsDevRow {
  return {
    model: 'model',
    key: 'vendor/model',
    source: 'site',
    provider_id: 'vendor',
    provider_name: 'Vendor',
    official: true,
    status: 'new',
    input: 3,
    output: 15,
    version: 'v1',
    current: noPrice,
    ...patch,
  }
}

const rows: ModelsDevRow[] = [
  row({
    model: 'claude-sonnet-4.5',
    key: 'anthropic/claude-sonnet-4-5',
    provider_name: 'Anthropic',
    cache_read: 0.3,
  }),
  row({
    model: 'gpt-5',
    key: 'openai/gpt-5',
    provider_name: 'OpenAI',
    status: 'update',
    input: 1.25,
    output: 10,
    current: {
      ...noPrice,
      priced: true,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", p * 1 + c * 8)',
    },
  }),
  row({
    model: 'qwen3-coder',
    key: 'hosta/qwen3-coder',
    provider_name: 'Host A',
    official: false,
    review: 'reseller',
    input: 0.3,
    output: 0.9,
  }),
  row({
    model: 'deepseek-v4.1-flash',
    key: 'deepseek/deepseek-v4.1-flash',
    provider_name: 'DeepSeek',
    status: 'locked',
    lock_reason: 'configured',
    input: 0.14,
    output: 0.28,
    current: {
      ...noPrice,
      priced: true,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("peak", p * 0.2857 + c * 1.1429)',
    },
  }),
  row({ model: 'glm-5', key: 'zai/glm-5', status: 'unchanged' }),
]
const plan: ModelsDevPlan = {
  hash: 'h1',
  catalog_hash: 'c1',
  fetched_at: 1760000000,
  rows,
  unmatched: ['my-private-model'],
  summary: { ...summary, created: 2, updated: 1, locked: 1, unchanged: 1 },
}

function entry(key: string, name: string): ModelsDevCatalogItem {
  const [provider = '', id = ''] = key.split('/')
  return {
    key,
    provider_id: provider,
    provider_name: provider,
    model_id: id,
    normalized_id: id,
    model_name: name,
    release_date: '2025-09-29',
    official: true,
    common: true,
    input: 3,
    output: 15,
    selected: false,
    excluded: false,
  }
}
const catalogItems = [
  entry('anthropic/claude-sonnet-4-5', 'Claude Sonnet 4.5'),
  entry('openai/gpt-5', 'GPT-5'),
]
const EXPRESSION = 'tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75)'

type Params = Record<string, unknown>
type Reply = Promise<{ data: unknown }>
type MockableApi = {
  get: (url: string, config?: { params?: Params }) => Reply
  put: (url: string, data?: unknown) => Reply
  post: (url: string, data?: unknown) => Reply
}

const apiClient = api as unknown as MockableApi
const originalApi = {
  get: apiClient.get,
  put: apiClient.put,
  post: apiClient.post,
}

interface Server {
  config: ModelsDevConfig
  state: ModelsDevStateView
  puts: ModelsDevConfig[]
  syncs: unknown[]
  undos: number
  gets: { url: string; params?: Params | undefined }[]
}

function ok(data: unknown) {
  return { data: { success: true, data } }
}

function installApi(
  initial: {
    config?: Partial<ModelsDevConfig>
    state?: Partial<ModelsDevStateView>
  } = {}
): Server {
  const server: Server = {
    config: config(initial.config),
    state: {
      last_sync_at: 0,
      last_sync_version: '',
      last_trigger: '',
      last_summary: summary,
      managed_count: 0,
      undo_count: 0,
      ...initial.state,
    },
    puts: [],
    syncs: [],
    undos: 0,
    gets: [],
  }
  apiClient.get = async (url, requestConfig) => {
    server.gets.push({ url, params: requestConfig?.params })
    if (url === `${BASE}/state`) {
      return ok({
        config: server.config,
        state: server.state,
        source_url: 'https://models.dev/api.json',
        current_version: 'v1.2.3',
        auto_sync_due: false,
      })
    }
    if (url === `${BASE}/catalog`) {
      return ok({
        total: catalogItems.length,
        items: catalogItems,
        providers: [
          { id: 'anthropic', name: 'Anthropic', count: 1 },
          { id: 'openai', name: 'OpenAI', count: 1 },
        ],
        fetched_at: 1760000000,
        hash: 'c1',
      })
    }
    if (url === `${BASE}/expression`) {
      return ok({ entry: catalogItems[0], expression: EXPRESSION })
    }
    throw new Error(`Unexpected GET ${url}`)
  }
  apiClient.put = async (url, data) => {
    expect(url).toBe(`${BASE}/config`)
    server.puts.push(data as ModelsDevConfig)
    server.config = data as ModelsDevConfig
    return ok(data)
  }
  apiClient.post = async (url, data) => {
    if (url === `${BASE}/preview`) return ok(plan)
    if (url === `${BASE}/sync`) {
      server.syncs.push(data)
      return ok({
        at: 1760000100,
        summary: { ...summary, created: 2, skipped_review: 1 },
        written: ['claude-sonnet-4.5', 'gpt-5'],
        skipped_review: ['qwen3-coder'],
      })
    }
    if (url === `${BASE}/undo`) {
      server.undos += 1
      return ok({ restored: ['claude-sonnet-4.5'], conflicts: [] })
    }
    throw new Error(`Unexpected POST ${url}`)
  }
  return server
}

let queryClient: InstanceType<typeof QueryClient> | null = null

function renderWith(children: React.ReactNode, language = i18n) {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={language}>{children}</I18nextProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  Object.assign(apiClient, originalApi)
  queryClient?.clear()
  queryClient = null
})

describe('turning the automatic import on', () => {
  test('explains the import and saves only after the confirmation', async () => {
    const server = installApi()
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('switch', { name: 'Import on every new release' })
    )
    const dialog = await screen.findByRole('alertdialog')
    expect(
      within(dialog).getByText(
        'Import prices from models.dev on every release?'
      )
    ).toBeVisible()
    expect(
      await within(dialog).findByText(
        /2 new prices and 1 updates\. 1 models stay locked and 1 wait for your approval/
      )
    ).toBeVisible()
    expect(server.puts).toHaveLength(0)

    await user.click(within(dialog).getByRole('button', { name: 'Turn on' }))
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(config({ auto_sync_enabled: true }))
  })

  test('cancelling the confirmation changes nothing', async () => {
    const server = installApi()
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('switch', { name: 'Import on every new release' })
    )
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(server.puts).toHaveLength(0)
    expect(
      screen.getByRole('switch', { name: 'Import on every new release' })
    ).not.toBeChecked()
  })

  test('turning it off needs no confirmation', async () => {
    const server = installApi({ config: { auto_sync_enabled: true } })
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('switch', { name: 'Import on every new release' })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(config({ auto_sync_enabled: false }))
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})

async function openPreview() {
  const server = installApi()
  renderWith(<ModelsDevSync />)
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Import now' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Import prices from models.dev',
  })
  await within(dialog).findByText('claude-sonnet-4.5')
  return { server, user, dialog }
}

describe('prices in the language of the console', () => {
  test('a console language that is not an Intl locale still formats prices', () => {
    expect(lib.formatUsd(0.2857, 'zhCN')).toContain('0.2857')
    expect(lib.formatUsd(15, 'zhTW')).toContain('15.00')
    expect(lib.formatUsd(undefined, 'zhCN')).toBe('-')
  })

  test('the preview opens in Simplified Chinese', async () => {
    installApi()
    renderWith(<ModelsDevSync />, chinese)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Import now' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Import prices from models.dev',
    })
    await within(dialog).findByText('claude-sonnet-4.5')
    await user.click(within(dialog).getByText('Locked (1)'))
    expect(
      await within(dialog).findByText(/Now: input US\$0\.2857/)
    ).toBeVisible()
  })
})

describe('the preview of an import', () => {
  test('lists the changes and leaves proposals that need a decision unticked', async () => {
    const { dialog } = await openPreview()

    expect(within(dialog).getByText('Changes (3)')).toBeVisible()
    expect(within(dialog).getByText('Locked (1)')).toBeVisible()
    expect(within(dialog).getByText('Up to date (1)')).toBeVisible()
    expect(within(dialog).getByText('Problems (0)')).toBeVisible()
    expect(
      within(dialog).getByRole('checkbox', { name: 'claude-sonnet-4.5' })
    ).toBeChecked()
    expect(
      within(dialog).getByRole('checkbox', { name: 'gpt-5' })
    ).toBeChecked()
    expect(
      within(dialog).getByRole('checkbox', { name: 'qwen3-coder' })
    ).not.toBeChecked()
    expect(within(dialog).getByText('Reseller price')).toBeVisible()
    // Only the row priced by a reseller carries the badge.
    expect(within(dialog).getByText('Reseller')).toBeVisible()
    expect(within(dialog).getByText('Price update')).toBeVisible()
    expect(
      within(dialog).getByText(/1 proposals wait for your decision/)
    ).toBeVisible()
    expect(
      within(dialog).getByRole('button', { name: 'Import 2 models' })
    ).toBeEnabled()
  })

  test('writes what needs no decision when nothing was changed', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(
      within(dialog).getByRole('button', { name: 'Import 2 models' })
    )
    await waitFor(() => expect(server.syncs).toHaveLength(1))
    expect(server.syncs[0]).toEqual({ plan_hash: 'h1' })
    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', { name: 'Import prices from models.dev' })
      ).not.toBeInTheDocument()
    )
  })

  test('sends the approval of a proposal that needed a decision', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(
      within(dialog).getByRole('checkbox', { name: 'qwen3-coder' })
    )
    await user.click(
      within(dialog).getByRole('button', { name: 'Import 3 models' })
    )
    await waitFor(() => expect(server.syncs).toHaveLength(1))
    expect(server.syncs[0]).toEqual({
      plan_hash: 'h1',
      approved: ['qwen3-coder'],
    })
  })

  test('limits the import to the rows that are still ticked', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(
      within(dialog).getByRole('checkbox', { name: 'claude-sonnet-4.5' })
    )
    await user.click(
      within(dialog).getByRole('button', { name: 'Import 1 models' })
    )
    await waitFor(() => expect(server.syncs).toHaveLength(1))
    expect(server.syncs[0]).toEqual({ plan_hash: 'h1', models: ['gpt-5'] })
  })

  test('a model with a price is locked, and can be allowed', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(within(dialog).getByText('Locked (1)'))
    expect(await within(dialog).findByText('deepseek-v4.1-flash')).toBeVisible()
    expect(within(dialog).getByText('Already has a price')).toBeVisible()
    expect(
      within(dialog).getByText(/Now: input \$0\.2857 · output \$1\.14/)
    ).toBeVisible()
    expect(
      within(dialog).queryByRole('checkbox', { name: 'deepseek-v4.1-flash' })
    ).not.toBeInTheDocument()

    await user.click(
      within(dialog).getByRole('button', {
        name: 'Actions for deepseek-v4.1-flash',
      })
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Allow import' })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(
      config({ unlocked_models: ['deepseek-v4.1-flash'] })
    )
  })

  test('a model can be kept out of every import', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(
      within(dialog).getByRole('button', { name: 'Actions for gpt-5' })
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Keep out of imports' })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(config({ locked_models: ['gpt-5'] }))
  })

  test('maps a model that models.dev does not know by name', async () => {
    const { server, user, dialog } = await openPreview()

    await user.click(
      within(dialog).getByRole('button', {
        name: /1 models of your site have no models\.dev price/,
      })
    )
    await user.click(
      await within(dialog).findByRole('button', {
        name: 'Map my-private-model',
      })
    )
    const picker = await screen.findByRole('dialog', {
      name: 'Map my-private-model to a models.dev model',
    })
    await user.click(
      await within(picker).findByRole('button', {
        name: 'Use this price: anthropic/claude-sonnet-4-5',
      })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(
      config({
        mappings: { 'my-private-model': 'anthropic/claude-sonnet-4-5' },
      })
    )
    const catalogCall = server.gets.find(
      (call) => call.url === `${BASE}/catalog`
    )
    expect(catalogCall?.params).toMatchObject({ q: 'my-private-model' })
  })
})

describe('choosing the models to import', () => {
  test('unticking a model the switches take adds it to the left-out ones', async () => {
    const server = installApi()
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Choose models' })
    )
    const dialog = await screen.findByRole('dialog', {
      name: 'Choose models to import',
    })
    const gpt = await within(dialog).findByRole('checkbox', {
      name: 'openai/gpt-5',
    })
    expect(gpt).toBeChecked()
    expect(
      within(dialog).getByRole('button', { name: 'Save selection' })
    ).toBeDisabled()

    await user.click(gpt)
    await user.click(
      within(dialog).getByRole('button', { name: 'Save selection' })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toEqual(config({ excluded_keys: ['openai/gpt-5'] }))
  })

  test('ticking a model the switches leave out picks it by hand', async () => {
    const server = installApi({
      config: { include_site_models: false, include_common_models: false },
    })
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Choose models' })
    )
    const dialog = await screen.findByRole('dialog', {
      name: 'Choose models to import',
    })
    const claude = await within(dialog).findByRole('checkbox', {
      name: 'anthropic/claude-sonnet-4-5',
    })
    expect(claude).not.toBeChecked()

    await user.click(claude)
    await user.click(
      within(dialog).getByRole('button', { name: 'Save selection' })
    )
    await waitFor(() => expect(server.puts).toHaveLength(1))
    expect(server.puts[0]).toMatchObject({
      selected_keys: ['anthropic/claude-sonnet-4-5'],
      excluded_keys: [],
    })
  })
})

describe('undoing the last import', () => {
  test('restores the prices after a confirmation', async () => {
    const server = installApi({
      state: { undo_count: 2, undo_at: 1760000100, last_sync_at: 1760000100 },
    })
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'More actions' })
    )
    await user.click(
      await screen.findByRole('menuitem', { name: 'Undo the last import' })
    )
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/2 models imported at/)).toBeVisible()
    expect(server.undos).toBe(0)
    await user.click(
      within(dialog).getByRole('button', { name: 'Undo import' })
    )
    await waitFor(() => expect(server.undos).toBe(1))
  })

  test('is not offered when there is nothing to undo', async () => {
    installApi()
    renderWith(<ModelsDevSync />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'More actions' })
    )
    expect(
      await screen.findByRole('menuitem', { name: 'Undo the last import' })
    ).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('importing one model into the editor', () => {
  test('hands back the expression of the entry that was picked', async () => {
    const server = installApi()
    const onExpression = vi.fn()
    renderWith(
      <ImportFromModelsDev
        modelName='claude-sonnet-4.5'
        onExpression={onExpression}
      />
    )
    const user = userEvent.setup()

    await user.click(
      screen.getByRole('button', { name: 'Import from models.dev' })
    )
    const picker = await screen.findByRole('dialog', {
      name: 'Import from models.dev',
    })
    await user.click(
      await within(picker).findByRole('button', {
        name: 'Use this price: anthropic/claude-sonnet-4-5',
      })
    )
    await waitFor(() => expect(onExpression).toHaveBeenCalledWith(EXPRESSION))
    const asked = server.gets.find((call) => call.url === `${BASE}/expression`)
    expect(asked?.params).toEqual({
      key: 'anthropic/claude-sonnet-4-5',
      model: 'claude-sonnet-4.5',
    })
    expect(
      server.gets.find((call) => call.url === `${BASE}/catalog`)?.params
    ).toMatchObject({ q: 'claude-sonnet-4.5' })
  })

  test('names the entry when the model has no name yet', async () => {
    const server = installApi()
    renderWith(<ImportFromModelsDev modelName='' onExpression={vi.fn()} />)
    const user = userEvent.setup()

    await user.click(
      screen.getByRole('button', { name: 'Import from models.dev' })
    )
    const picker = await screen.findByRole('dialog', {
      name: 'Import from models.dev',
    })
    await user.click(
      await within(picker).findByRole('button', {
        name: 'Use this price: openai/gpt-5',
      })
    )
    await waitFor(() =>
      expect(
        server.gets.some((call) => call.url === `${BASE}/expression`)
      ).toBe(true)
    )
    expect(
      server.gets.find((call) => call.url === `${BASE}/expression`)?.params
    ).toEqual({ key: 'openai/gpt-5', model: 'gpt-5' })
  })
})

describe('the choices of the administrator', () => {
  const item = { key: 'openai/gpt-5', common: true, site_models: [] }
  const empty = { selected: new Set<string>(), excluded: new Set<string>() }

  test('a hand pick always counts, an exclusion only removes automatic choices', () => {
    const on = config()
    const off = config({ include_common_models: false })
    const left = { ...empty, excluded: new Set([item.key]) }
    expect(lib.isIncluded(item, on, empty)).toBe(true)
    expect(lib.isIncluded(item, off, empty)).toBe(false)
    expect(lib.isIncluded(item, on, left)).toBe(false)
    expect(
      lib.isIncluded(item, off, { ...empty, selected: new Set([item.key]) })
    ).toBe(true)
    const both = {
      selected: new Set([item.key]),
      excluded: new Set([item.key]),
    }
    expect(lib.isIncluded(item, on, both)).toBe(true)
  })

  test('ticking and unticking keep the two lists apart', () => {
    const on = config()
    const off = config({ include_common_models: false })
    const left = lib.setEntryIncluded(item, on, empty, false)
    expect([...left.excluded]).toEqual([item.key])
    expect(left.selected.size).toBe(0)
    const back = lib.setEntryIncluded(item, on, left, true)
    expect(back.excluded.size).toBe(0)
    expect(back.selected.size).toBe(0)
    const picked = lib.setEntryIncluded(item, off, empty, true)
    expect([...picked.selected]).toEqual([item.key])
    const dropped = lib.setEntryIncluded(item, off, picked, false)
    expect(lib.sameChoice(dropped, empty)).toBe(true)
  })

  test('a lock and an allowance exclude each other', () => {
    const locked = lib.keepOut(config({ unlocked_models: ['a'] }), 'a')
    expect(locked.locked_models).toEqual(['a'])
    expect(locked.unlocked_models).toEqual([])
    const allowed = lib.allowImport(locked, 'a')
    expect(allowed.locked_models).toEqual([])
    expect(allowed.unlocked_models).toEqual(['a'])
    expect(lib.withMapping(config(), 'a', 'x/y').mappings).toEqual({ a: 'x/y' })
    const mapped = config({ mappings: { a: 'x/y' } })
    expect(lib.withMapping(mapped, 'a', null)).toEqual(config())
  })

  test('a complete import leaves out the model list', () => {
    const everything = lib.defaultChecked(plan)
    expect([...everything].sort()).toEqual(['claude-sonnet-4.5', 'gpt-5'])
    expect(lib.buildSyncRequest(plan, everything)).toEqual({ plan_hash: 'h1' })
    const some = lib.buildSyncRequest(plan, new Set(['gpt-5', 'qwen3-coder']))
    expect(some).toEqual({
      plan_hash: 'h1',
      models: ['gpt-5', 'qwen3-coder'],
      approved: ['qwen3-coder'],
    })
    const all = new Set([...everything, 'qwen3-coder'])
    expect(lib.buildSyncRequest(plan, all)).toEqual({
      plan_hash: 'h1',
      approved: ['qwen3-coder'],
    })
  })
})
