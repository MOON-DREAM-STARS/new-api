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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  PriceComparisonAdminView,
  PriceComparisonSettings,
  PriceComparisonView,
} from './types'

type ApiResult<T> = { success: boolean; message?: string; data: T }

export const priceComparisonQueryKey = ['price-comparison', 'view'] as const
export const priceComparisonAdminQueryKey = [
  'price-comparison',
  'admin',
] as const

export async function getPriceComparison(): Promise<PriceComparisonView> {
  const response = await api.get<ApiResult<PriceComparisonView>>(
    '/api/price_comparison'
  )
  return requireServerSuccess(response.data).data
}

/** The choices the administrator can make; administrators only. */
export async function getPriceComparisonAdmin(): Promise<PriceComparisonAdminView> {
  const response = await api.get<ApiResult<PriceComparisonAdminView>>(
    '/api/price_comparison/settings'
  )
  return requireServerSuccess(response.data).data
}

export async function savePriceComparisonSettings(
  settings: PriceComparisonSettings
): Promise<PriceComparisonSettings> {
  const response = await api.put<ApiResult<PriceComparisonSettings>>(
    '/api/price_comparison/settings',
    settings,
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}
