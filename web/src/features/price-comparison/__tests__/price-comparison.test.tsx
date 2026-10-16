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
import { afterEach, describe, expect, test } from 'vitest'

import type { PriceConfig } from '../types'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { ROLE } = await import('@/lib/roles')
const { useAuthStore } = await import('@/stores/auth-store')
const { combineBillingExpr } =
  await import('@/features/pricing/lib/billing-expr')
const { PriceComparison } = await import('../index')
const prices = await import('../lib/prices')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

// DeepSeek style: ¥2 / ¥8 per million tokens at 7 yuan per dollar during
// the day, half of it at night.
const dayAndNight =
  'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 21 ? tier("peak", p * 0.2857142857 + c * 1.1428571429) : tier("off_peak", p * 0.1428571429 + c * 0.5714285714)'

function ratioPrice(modelRatio: number, completionRatio: number): PriceConfig {
  return {
    priced: true,
    quota_type: 0,
    model_ratio: modelRatio,
    model_price: 0,
    completion_ratio: completionRatio,
  }
}

function expressionPrice(expression: string): PriceConfig {
  return {
    ...ratioPrice(0, 0),
    billing_mode: 'tiered_expr',
    billing_expr: expression,
  }
}

function listPriceOf(config: PriceConfig) {
  const list = prices.readListPrice(config)
  if (!list) throw new Error('expected a list price')
  return list
}

describe('reading list prices', () => {
  test('ratio billing: one ratio is two dollars per million input tokens', () => {
    const list = prices.readListPrice(ratioPrice(0.25, 4))
    expect(list?.peak).toEqual({ input: 0.5, output: 2 })
    expect(list?.timeBased).toBe(false)
    expect(prices.blendedPrice({ input: 0.5, output: 2 })).toBeCloseTo(0.875)
  })

  test('time of day billing: the peak and the off-peak price', () => {
    const list = listPriceOf(expressionPrice(dayAndNight))
    expect(list.timeBased).toBe(true)
    expect(list.peak.input).toBeCloseTo(0.2857, 4)
    expect(list.offPeak.input).toBeCloseTo(0.1429, 4)
    expect(prices.blendedPrice(list.peak)).toBeCloseTo(0.5, 6)
    expect(prices.blendedPrice(list.offPeak)).toBeCloseTo(0.25, 6)
  })

  test('context tiers: the shortest context is the entry price', () => {
    const list = prices.readListPrice(
      expressionPrice(
        'len <= 200000 ? tier("standard", p * 1.25 + c * 10) : tier("long_context", p * 2.5 + c * 15)'
      )
    )
    expect(list?.peak).toEqual({ input: 1.25, output: 10 })
    expect(list?.timeBased).toBe(false)
  })

  test('request rules do not change the list price', () => {
    const expression = combineBillingExpr(
      'tier("base", p * 2 + c * 8)',
      '(param("service_tier") == "fast" ? 2 : 1)'
    )
    const list = prices.readListPrice(expressionPrice(expression))
    expect(list?.peak).toEqual({ input: 2, output: 8 })
  })

  test('an expression wins over a fixed price, as in billing', () => {
    const list = prices.readListPrice({
      ...expressionPrice('tier("base", p * 3 + c * 15)'),
      quota_type: 1,
      model_price: 0.04,
    })
    expect(list?.peak).toEqual({ input: 3, output: 15 })
  })

  test.each([
    [
      'a fixed price per request',
      { ...ratioPrice(0, 0), quota_type: 1, model_price: 0.04 },
    ],
    [
      'a price per request in an expression',
      expressionPrice('tier("request", fixed(0.01))'),
    ],
    ['a free model', ratioPrice(0, 1)],
    ['a model without a price', { ...ratioPrice(1, 1), priced: false }],
    ['an expression that is not valid', expressionPrice('p *')],
  ])('%s cannot be compared', (_name, config) => {
    expect(prices.readListPrice(config)).toBeNull()
    expect(prices.isComparable(config)).toBe(false)
  })

  test('the tier or group ratio scales every price, a free group does not', () => {
    const list = listPriceOf(ratioPrice(0.25, 4))
    expect(prices.shownPrice(list, 'peak', 0.15)).toMatchObject({
      input: 0.075,
      output: 0.3,
    })
    expect(prices.shownPrice(list, 'peak', 0).input).toBe(0.5)
  })

  test('the multiple is the blended price over the baseline blended price', () => {
    const baseline = prices.shownPrice(
      listPriceOf(expressionPrice(dayAndNight)),
      'peak',
      1
    )
    const model = prices.shownPrice(listPriceOf(ratioPrice(0.5, 5)), 'peak', 1)
    expect(prices.priceMultiple(model, baseline)).toBeCloseTo(4, 6)
    expect(prices.priceMultiple(model, null)).toBeNull()
  })
})

describe('formatting', () => {
  test.each([
    [0.4, 'multiplier', '0.40X'],
    [1, 'multiplier', '1.00X'],
    [0.0286, 'multiplier', '0.029X'],
    [12.34, 'multiplier', '12.3X'],
    [125, 'multiplier', '125X'],
    [0.4, 'percent', '40%'],
    [0.0286, 'percent', '2.86%'],
    [0.4286, 'percent', '42.9%'],
    [1.25, 'percent', '125%'],
  ] as const)('%s as %s is %s', (value, format, expected) => {
    expect(prices.formatMultiple(value, format, 'en')).toBe(expected)
  })

  test('money keeps the precision small prices need', () => {
    expect(prices.formatMoney(0.5, 'USD', 7, 'en')).toBe('$0.50')
    expect(prices.formatMoney(0.2857, 'USD', 7, 'en')).toBe('$0.2857')
    expect(prices.formatMoney(15, 'USD', 7, 'en')).toBe('$15.00')
    expect(prices.formatMoney(0.5, 'CNY', 7, 'zh-CN')).toBe('¥3.50')
  })
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod; put: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalApi = { get: apiClient.get, put: apiClient.put }
const originalAuth = useAuthStore.getState().auth

const requestPriced: PriceConfig = {
  ...ratioPrice(0, 0),
  quota_type: 1,
  model_price: 0.04,
}
const flash = {
  model_name: 'deepseek-v4.1-flash',
  price: expressionPrice(dayAndNight),
}
const settings = {
  baseline_model: 'deepseek-v4.1-flash',
  price_basis: 'peak',
  currency: 'USD',
  ratio_format: 'multiplier',
  families: [{ family_id: 4, models: ['deepseek-v4-pro'] }],
}
const pageData = {
  success: true,
  data: {
    settings,
    usd_exchange_rate: 7,
    baseline: { ...flash, price_ratio: 1 },
    families: [
      {
        id: 4,
        key: 'deepseek',
        name: 'DeepSeek',
        models: [
          { ...flash, price_ratio: 0.15, tier: 'economy' },
          {
            model_name: 'deepseek-v4-pro',
            price: ratioPrice(1, 4),
            price_ratio: 1,
          },
        ],
      },
      {
        id: 1,
        key: 'claude',
        name: 'Claude',
        models: [
          {
            model_name: 'claude-haiku',
            price: ratioPrice(0.5, 5),
            price_ratio: 1,
          },
          { model_name: 'claude-image', price: requestPriced, price_ratio: 1 },
        ],
      },
    ],
  },
}
const adminData = {
  success: true,
  data: {
    settings,
    usd_exchange_rate: 7,
    families: [
      { id: 4, key: 'deepseek', name: 'DeepSeek' },
      { id: 1, key: 'claude', name: 'Claude' },
    ],
    models: [
      { ...flash, family_id: 4, enabled: true },
      {
        model_name: 'deepseek-v4-pro',
        price: ratioPrice(1, 4),
        family_id: 4,
        enabled: true,
      },
      {
        model_name: 'claude-haiku',
        price: ratioPrice(0.5, 5),
        family_id: 1,
        enabled: true,
      },
      {
        model_name: 'claude-image',
        price: requestPriced,
        family_id: 1,
        enabled: true,
      },
    ],
  },
}

function installApi(): unknown[] {
  const saved: unknown[] = []
  apiClient.get = async (url) => {
    if (url === '/api/price_comparison') return { data: pageData }
    if (url === '/api/price_comparison/settings') return { data: adminData }
    throw new Error(`Unexpected GET ${url}`)
  }
  apiClient.put = async (url, data) => {
    expect(url).toBe('/api/price_comparison/settings')
    saved.push(data)
    return { data: { success: true, data } }
  }
  return saved
}

function signInAs(role: number) {
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 2, username: 'tester', role },
    },
  })
}

let queryClient: InstanceType<typeof QueryClient> | null = null

function renderPage() {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <PriceComparison />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  Object.assign(apiClient, originalApi)
  useAuthStore.setState({ auth: originalAuth })
  queryClient?.clear()
  queryClient = null
})

describe('the price comparison page', () => {
  test('shows the baseline and the lowest price of every chosen model', async () => {
    signInAs(ROLE.USER)
    installApi()
    renderPage()

    const cards = await screen.findAllByRole('group', {
      name: 'deepseek-v4.1-flash',
    })
    expect(cards).toHaveLength(2)
    const [baselineCard, economyCard] = cards
    expect(within(baselineCard).getByText('Baseline')).toBeVisible()
    expect(within(baselineCard).getByText('1.00X')).toBeVisible()
    expect(within(baselineCard).getByText('$0.50 per 1M tokens')).toBeVisible()
    expect(
      within(baselineCard).getByText('Off-peak: $0.25 per 1M tokens')
    ).toBeVisible()
    // The economy tier ratio replaces the list price: 0.15 of the baseline.
    expect(within(economyCard).getByText('0.15X')).toBeVisible()
    expect(within(economyCard).getByText('Economy tier')).toBeVisible()
    expect(within(economyCard).getByText('$0.075 per 1M tokens')).toBeVisible()

    const haiku = screen.getByRole('group', { name: 'claude-haiku' })
    expect(within(haiku).getByText('4.00X')).toBeVisible()
    expect(within(haiku).getByText('Standard price')).toBeVisible()
    expect(within(haiku).getByText('$2.00 per 1M tokens')).toBeVisible()
    expect(within(haiku).getByText('$1.00')).toBeVisible()
    expect(within(haiku).getByText('$5.00')).toBeVisible()
    const pro = screen.getByRole('group', { name: 'deepseek-v4-pro' })
    expect(within(pro).getByText('7.00X')).toBeVisible()
    // A price per request has nothing to compare.
    expect(screen.queryByRole('group', { name: 'claude-image' })).toBeNull()
  })

  test('regular users cannot change the page', async () => {
    signInAs(ROLE.USER)
    installApi()
    renderPage()
    await screen.findByText('Claude')
    expect(screen.queryByRole('button', { name: 'Page settings' })).toBeNull()
  })

  test('administrators choose the models and the display of the page', async () => {
    signInAs(ROLE.ADMIN)
    const saved = installApi()
    renderPage()
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Page settings' })
    )
    const dialog = await screen.findByRole('dialog', {
      name: 'Price comparison settings',
    })
    const save = within(dialog).getByRole('button', { name: 'Save' })
    await within(dialog).findByRole('region', { name: 'Claude' })
    expect(save).toBeDisabled()

    await user.click(
      within(dialog).getByRole('button', { name: 'Chinese yuan' })
    )
    expect(
      within(dialog).getByText(
        'Yuan prices use the exchange rate of this site: 1 USD = 7 CNY.'
      )
    ).toBeVisible()

    // A model that is priced per request cannot be added.
    await user.click(
      within(dialog).getByRole('combobox', { name: 'Add a model to Claude' })
    )
    expect(
      screen.getByRole('option', { name: /claude-image/ })
    ).toHaveAttribute('aria-disabled', 'true')
    await user.click(screen.getByRole('option', { name: /claude-haiku/ }))
    const claude = within(dialog).getByRole('region', { name: 'Claude' })
    expect(within(claude).getByText('claude-haiku')).toBeVisible()

    // Models are listed in the order the administrator arranged them.
    await user.click(
      within(dialog).getByRole('combobox', { name: 'Add a model to DeepSeek' })
    )
    await user.click(
      screen.getByRole('option', { name: /deepseek-v4\.1-flash/ })
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Move deepseek-v4.1-flash up',
      })
    )

    expect(save).toBeEnabled()
    await user.click(save)
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toEqual({
      baseline_model: 'deepseek-v4.1-flash',
      price_basis: 'peak',
      currency: 'CNY',
      ratio_format: 'multiplier',
      families: [
        { family_id: 4, models: ['deepseek-v4.1-flash', 'deepseek-v4-pro'] },
        { family_id: 1, models: ['claude-haiku'] },
      ],
    })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })
})
