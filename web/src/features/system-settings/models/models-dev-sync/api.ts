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
  ModelsDevCatalogPage,
  ModelsDevCatalogParams,
  ModelsDevConfig,
  ModelsDevExpressionResult,
  ModelsDevOverview,
  ModelsDevPlan,
  ModelsDevSyncRequest,
  ModelsDevSyncResult,
  ModelsDevUndoResult,
} from './types'

type ApiResult<T> = { success: boolean; message?: string; data: T }

const BASE = '/api/option/models_dev'
// Mutations show their own failures, so the shared client stays quiet.
const QUIET = { skipBusinessError: true, skipErrorHandler: true } as const

export const modelsDevKeys = {
  all: ['models-dev'] as const,
  overview: ['models-dev', 'overview'] as const,
  preview: ['models-dev', 'preview'] as const,
  catalog: (params: ModelsDevCatalogParams) =>
    ['models-dev', 'catalog', params] as const,
}

export async function getModelsDevOverview(): Promise<ModelsDevOverview> {
  const response = await api.get<ApiResult<ModelsDevOverview>>(`${BASE}/state`)
  return requireServerSuccess(response.data).data
}

export async function saveModelsDevConfig(
  config: ModelsDevConfig
): Promise<ModelsDevConfig> {
  const response = await api.put<ApiResult<ModelsDevConfig>>(
    `${BASE}/config`,
    config,
    QUIET
  )
  return requireServerSuccess(response.data).data
}

export async function getModelsDevCatalog(
  params: ModelsDevCatalogParams
): Promise<ModelsDevCatalogPage> {
  const response = await api.get<ApiResult<ModelsDevCatalogPage>>(
    `${BASE}/catalog`,
    {
      params: {
        q: params.q || undefined,
        provider: params.provider || undefined,
        common: params.common ? 1 : undefined,
        official: params.official ? 1 : undefined,
        limit: params.limit,
        offset: params.offset,
        refresh: params.refresh ? 1 : undefined,
      },
      skipErrorHandler: true,
    }
  )
  return requireServerSuccess(response.data).data
}

/** The billing expression of one entry, written for one model name. */
export async function getModelsDevExpression(
  key: string,
  model: string
): Promise<ModelsDevExpressionResult> {
  const response = await api.get<ApiResult<ModelsDevExpressionResult>>(
    `${BASE}/expression`,
    { params: { key, model }, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function previewModelsDev(
  refresh = false
): Promise<ModelsDevPlan> {
  const response = await api.post<ApiResult<ModelsDevPlan>>(
    `${BASE}/preview`,
    undefined,
    { params: { refresh: refresh ? 1 : undefined }, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function syncModelsDev(
  request: ModelsDevSyncRequest
): Promise<ModelsDevSyncResult> {
  const response = await api.post<ApiResult<ModelsDevSyncResult>>(
    `${BASE}/sync`,
    request,
    QUIET
  )
  return requireServerSuccess(response.data).data
}

export async function undoModelsDevSync(): Promise<ModelsDevUndoResult> {
  const response = await api.post<ApiResult<ModelsDevUndoResult>>(
    `${BASE}/undo`,
    undefined,
    QUIET
  )
  return requireServerSuccess(response.data).data
}
