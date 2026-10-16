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
export type PriceBasis = 'peak' | 'off_peak'
export type PriceCurrency = 'USD' | 'CNY'
export type PriceFormat = 'multiplier' | 'percent'

export interface PriceComparisonFamilyPick {
  family_id: number
  models: string[]
}

/** What the administrator chose for the page. */
export interface PriceComparisonSettings {
  baseline_model: string
  price_basis: PriceBasis
  currency: PriceCurrency
  ratio_format: PriceFormat
  families: PriceComparisonFamilyPick[]
}

/** The pricing of one model, as much as the browser needs to read its list price. */
export interface PriceConfig {
  priced: boolean
  quota_type: number
  model_ratio: number
  model_price: number
  completion_ratio: number
  billing_mode?: string
  billing_expr?: string
}

export interface PriceComparisonModel {
  model_name: string
  price: PriceConfig
  /** Multiplies the list price: the lowest service tier ratio or group ratio. */
  price_ratio: number
  /** The service tier behind price_ratio; empty for plain routing. */
  tier?: string
}

export interface PriceComparisonFamily {
  id: number
  key: string
  name: string
  models: PriceComparisonModel[]
}

export interface PriceComparisonView {
  settings: PriceComparisonSettings
  usd_exchange_rate: number
  baseline: PriceComparisonModel
  families: PriceComparisonFamily[]
}

export interface PriceComparisonAdminFamily {
  id: number
  key: string
  name: string
}

export interface PriceComparisonAdminModel {
  model_name: string
  price: PriceConfig
  /** True when at least one group offers the model. */
  enabled: boolean
  /** 0 for models that belong to no family. */
  family_id: number
}

export interface PriceComparisonAdminView {
  settings: PriceComparisonSettings
  usd_exchange_rate: number
  families: PriceComparisonAdminFamily[]
  models: PriceComparisonAdminModel[]
}
