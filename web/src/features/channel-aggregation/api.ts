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
  AggregationChannel,
  AggregationOverview,
  FamilyDetail,
  FamilyInput,
  ModelFamily,
  PolicyInput,
  ServerMessage,
  Simulation,
  SimulationInput,
} from './types'

type ApiResult<T> = { success: boolean; message?: string; data: T }

// Mutations report failures through their own handlers.
const mutationConfig = { skipBusinessError: true, skipErrorHandler: true }

export async function getAggregationOverview(): Promise<AggregationOverview> {
  const response = await api.get<ApiResult<AggregationOverview>>(
    '/api/channel_aggregation/overview'
  )
  return requireServerSuccess(response.data).data
}

export async function getFamilyDetail(id: number): Promise<FamilyDetail> {
  const response = await api.get<ApiResult<FamilyDetail>>(
    `/api/channel_aggregation/families/${id}`
  )
  return requireServerSuccess(response.data).data
}

export async function getChannelOptions(
  familyId: number,
  model: string
): Promise<AggregationChannel[]> {
  const params = new URLSearchParams({ family_id: String(familyId), model })
  const response = await api.get<ApiResult<AggregationChannel[]>>(
    `/api/channel_aggregation/channels?${params.toString()}`
  )
  return requireServerSuccess(response.data).data
}

export async function createFamily(input: FamilyInput): Promise<ModelFamily> {
  const response = await api.post<ApiResult<ModelFamily>>(
    '/api/channel_aggregation/families',
    input,
    mutationConfig
  )
  return requireServerSuccess(response.data).data
}

export async function updateFamily(
  id: number,
  input: FamilyInput
): Promise<{ version: number }> {
  const response = await api.put<ApiResult<{ version: number }>>(
    `/api/channel_aggregation/families/${id}`,
    input,
    mutationConfig
  )
  return requireServerSuccess(response.data).data
}

export async function deleteFamily(id: number, version: number): Promise<void> {
  const response = await api.delete<ApiResult<null>>(
    `/api/channel_aggregation/families/${id}?version=${version}`,
    mutationConfig
  )
  requireServerSuccess(response.data)
}

export async function updateFamilyMembers(
  id: number,
  models: string[],
  version: number
): Promise<{ version: number; removed_overrides: number }> {
  const response = await api.put<
    ApiResult<{ version: number; removed_overrides: number }>
  >(
    `/api/channel_aggregation/families/${id}/members`,
    { models, version },
    mutationConfig
  )
  return requireServerSuccess(response.data).data
}

export async function savePolicy(
  input: PolicyInput
): Promise<{ id: number; version: number; warnings: ServerMessage[] }> {
  const response = await api.put<
    ApiResult<{ id: number; version: number; warnings: ServerMessage[] }>
  >('/api/channel_aggregation/policy', input, mutationConfig)
  return requireServerSuccess(response.data).data
}

export async function deletePolicy(
  familyId: number,
  model: string,
  version: number
): Promise<void> {
  const params = new URLSearchParams({
    family_id: String(familyId),
    model,
    version: String(version),
  })
  const response = await api.delete<ApiResult<null>>(
    `/api/channel_aggregation/policy?${params.toString()}`,
    mutationConfig
  )
  requireServerSuccess(response.data)
}

export async function simulateRoute(
  input: SimulationInput
): Promise<Simulation> {
  const response = await api.post<ApiResult<Simulation>>(
    '/api/channel_aggregation/simulate',
    input,
    mutationConfig
  )
  return requireServerSuccess(response.data).data
}
