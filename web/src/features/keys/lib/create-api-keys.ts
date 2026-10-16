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
import { createApiKey } from '../api'
import type { ApiKey, ApiKeyFormData, ApiResponse } from '../types'

/** Keys the backend reveals per request. */
export const REVEAL_BATCH_SIZE = 100

type CreateApiKeyFn = (data: ApiKeyFormData) => Promise<ApiResponse<ApiKey>>

export type NewApiKey = { id: number; name: string }

export type CreateApiKeysResult = {
  created: NewApiKey[]
  /** The response or error that stopped the creation, if any. */
  failure: unknown
  stopped: boolean
}

function randomSuffix(): string {
  return Math.random().toString(36).slice(2, 8)
}

/** The first key keeps the given name; the others get a random suffix. */
export function batchKeyName(
  baseName: string,
  index: number,
  suffix: () => string = randomSuffix
): string {
  if (index === 0 && baseName) return baseName
  return `${baseName || 'default'}-${suffix()}`
}

/**
 * Creates the keys one after another and stops at the first failure, so the
 * keys created before it are still reported.
 */
export async function createApiKeys(params: {
  count: number
  payload: ApiKeyFormData
  create?: CreateApiKeyFn
  suffix?: () => string
}): Promise<CreateApiKeysResult> {
  const create = params.create ?? createApiKey
  const created: NewApiKey[] = []
  for (let index = 0; index < params.count; index++) {
    const name = batchKeyName(params.payload.name, index, params.suffix)
    try {
      const result = await create({ ...params.payload, name })
      if (!result.success || !result.data) {
        return { created, failure: result, stopped: true }
      }
      created.push({ id: result.data.id, name: result.data.name || name })
    } catch (error) {
      return { created, failure: error, stopped: true }
    }
  }
  return { created, failure: null, stopped: false }
}

/** Reveals the full keys in the batches the backend accepts. */
export async function revealApiKeys(
  ids: number[],
  resolveBatch: (ids: number[]) => Promise<Record<number, string>>
): Promise<Record<number, string>> {
  const keys: Record<number, string> = {}
  for (let start = 0; start < ids.length; start += REVEAL_BATCH_SIZE) {
    Object.assign(
      keys,
      await resolveBatch(ids.slice(start, start + REVEAL_BATCH_SIZE))
    )
  }
  return keys
}
