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
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
} from '@/features/pricing/lib/billing-expr'
import { readTimeTokenPricing } from '@/features/pricing/lib/billing-expression/display'

import type {
  PriceBasis,
  PriceComparisonModel,
  PriceConfig,
  PriceCurrency,
  PriceFormat,
} from '../types'

/** Dollars per one million tokens. */
export interface TokenPrice {
  input: number
  output: number
}

/**
 * The list price of a model. Models that charge by time of day have a peak
 * and a lower off-peak price; the two are equal for every other model.
 */
export interface ListPrice {
  peak: TokenPrice
  offPeak: TokenPrice
  timeBased: boolean
}

/** A price after the tier or group ratio of the user. */
export interface ShownPrice extends TokenPrice {
  blended: number
}

export interface ModelPriceView {
  model: PriceComparisonModel
  list: ListPrice
  /** The price for the period the administrator chose. */
  shown: ShownPrice
  /** The price of the other period, only for models that charge by time of day. */
  alternate: ShownPrice | null
}

/** Weight of the input price in the blended price: 3 input tokens per output token. */
const INPUT_WEIGHT = 3
/** A model ratio of 1 means $2 per one million input tokens. */
const USD_PER_MILLION_AT_RATIO_ONE = 2

/** The 3:1 blended price of one million tokens, input and output together. */
export function blendedPrice(price: TokenPrice): number {
  return (INPUT_WEIGHT * price.input + price.output) / (INPUT_WEIGHT + 1)
}

function tokenPrice(input: unknown, output: unknown): TokenPrice | null {
  if (typeof input !== 'number' || !Number.isFinite(input) || input < 0) {
    return null
  }
  const completion =
    typeof output === 'number' && Number.isFinite(output) && output >= 0
      ? output
      : 0
  // A free model has nothing to compare.
  if (input === 0 && completion === 0) return null
  return { input, output: completion }
}

function fromPrices(prices: TokenPrice[]): ListPrice | null {
  const first = prices[0]
  if (!first) return null
  let peak = first
  let offPeak = first
  for (const price of prices) {
    if (blendedPrice(price) > blendedPrice(peak)) peak = price
    if (blendedPrice(price) < blendedPrice(offPeak)) offPeak = price
  }
  return {
    peak,
    offPeak,
    timeBased: blendedPrice(peak) > blendedPrice(offPeak),
  }
}

function readExpressionPrice(expression: string): ListPrice | null {
  const { billingExpr } = splitBillingExprAndRequestRules(expression)
  const timed = readTimeTokenPricing(billingExpr)
  if (timed) {
    // Every time-of-day branch is one leaf; the first token tier of a leaf is
    // its entry price (shorter contexts come first). Tiers of one leaf share
    // the same list of time conditions.
    const leaves = new Map<object, TokenPrice>()
    for (const tier of timed.tiers) {
      if (tier.billingUnit === 'request' || tier.imageCount) continue
      if (leaves.has(tier.timeConditions)) continue
      const price = tokenPrice(tier.prices.p, tier.prices.c)
      if (price) leaves.set(tier.timeConditions, price)
    }
    return fromPrices([...leaves.values()])
  }
  const tier = parseTiersFromExpr(billingExpr).find(
    (item) => item.billingUnit !== 'request' && !item.imageCount
  )
  const price = tier ? tokenPrice(tier.inputPrice, tier.outputPrice) : null
  return price ? { peak: price, offPeak: price, timeBased: false } : null
}

function readRatioPrice(config: PriceConfig): ListPrice | null {
  // A fixed price per request cannot be compared with a price per token.
  if (config.quota_type === 1 || !(config.model_ratio > 0)) return null
  const input = config.model_ratio * USD_PER_MILLION_AT_RATIO_ONE
  const price = tokenPrice(input, input * config.completion_ratio)
  return price ? { peak: price, offPeak: price, timeBased: false } : null
}

/**
 * Reads the list price of a model in dollars per million tokens, or returns
 * null when the model has no token price to compare: priced per request, free,
 * or billed by an expression this page cannot read.
 */
export function readListPrice(config: PriceConfig): ListPrice | null {
  if (!config.priced) return null
  // A billing expression wins over ratios and fixed prices, as in billing.
  if (config.billing_mode === 'tiered_expr' && config.billing_expr) {
    return readExpressionPrice(config.billing_expr)
  }
  return readRatioPrice(config)
}

export function isComparable(config: PriceConfig): boolean {
  return readListPrice(config) !== null
}

function scale(price: TokenPrice, ratio: number): ShownPrice {
  // A zero or negative ratio is a free group, not a price to compare.
  const factor = ratio > 0 ? ratio : 1
  const input = price.input * factor
  const output = price.output * factor
  return { input, output, blended: blendedPrice({ input, output }) }
}

export function shownPrice(
  list: ListPrice,
  basis: PriceBasis,
  ratio: number
): ShownPrice {
  return scale(basis === 'off_peak' ? list.offPeak : list.peak, ratio)
}

/** The prices to show for a model, or null when it cannot be compared. */
export function buildModelPriceView(
  model: PriceComparisonModel,
  basis: PriceBasis
): ModelPriceView | null {
  const list = readListPrice(model.price)
  if (!list) return null
  return {
    model,
    list,
    shown: shownPrice(list, basis, model.price_ratio),
    alternate: list.timeBased
      ? shownPrice(
          list,
          basis === 'peak' ? 'off_peak' : 'peak',
          model.price_ratio
        )
      : null,
  }
}

/** How many times the baseline price a price is; null without a usable baseline. */
export function priceMultiple(
  price: ShownPrice,
  baseline: ShownPrice | null | undefined
): number | null {
  if (!baseline || !(baseline.blended > 0)) return null
  return price.blended / baseline.blended
}

function moneyDigits(amount: number): number {
  const magnitude = Math.abs(amount)
  if (magnitude >= 1) return 2
  if (magnitude >= 0.01) return 4
  return 6
}

/**
 * Formats dollars in the currency the administrator chose. The page currency
 * is independent of the display currency of the site, so the exchange rate of
 * the site converts dollars to yuan here.
 */
export function formatMoney(
  usd: number,
  currency: PriceCurrency,
  exchangeRate: number,
  locale?: string
): string {
  const amount =
    currency === 'CNY' ? usd * (exchangeRate > 0 ? exchangeRate : 1) : usd
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: moneyDigits(amount),
  }).format(amount)
}

/** "0.40X" or "40%": how the price compares with the baseline. */
export function formatMultiple(
  multiple: number,
  format: PriceFormat,
  locale?: string
): string {
  if (format === 'percent') {
    const percent = multiple * 100
    let digits = 2
    if (percent >= 100) digits = 0
    else if (percent >= 10) digits = 1
    return new Intl.NumberFormat(locale, {
      style: 'percent',
      maximumFractionDigits: digits,
    }).format(multiple)
  }
  let digits = { min: 2, max: 3 }
  if (multiple >= 100) digits = { min: 0, max: 0 }
  else if (multiple >= 10) digits = { min: 0, max: 1 }
  else if (multiple >= 0.1) digits = { min: 2, max: 2 }
  const text = new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits.min,
    maximumFractionDigits: digits.max,
  }).format(multiple)
  return `${text}X`
}
