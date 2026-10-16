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
  ServiceTierPreview,
  ServiceTierSettings,
  ServiceTierUserOverview,
} from './types'

type ApiResult<T> = { success: boolean; message?: string; data: T }

/** Shared by the service tiers page and the API key wizard. */
export const serviceTierOverviewQueryKey = ['service-tier', 'overview'] as const

export async function getServiceTierOverview(): Promise<ServiceTierUserOverview> {
  const response = await api.get<ApiResult<ServiceTierUserOverview>>(
    '/api/user/service_tier'
  )
  return requireServerSuccess(response.data).data
}

export async function previewServiceTierSettings(
  settings: ServiceTierSettings,
  signal?: AbortSignal
): Promise<ServiceTierPreview> {
  const response = await api.post<ApiResult<ServiceTierPreview>>(
    '/api/user/service_tier/preview',
    settings,
    { signal, skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function saveServiceTierSettings(
  settings: ServiceTierSettings
): Promise<ServiceTierUserOverview> {
  const response = await api.put<ApiResult<ServiceTierUserOverview>>(
    '/api/user/service_tier',
    settings,
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}
