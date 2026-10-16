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
import { describe, expect, test, vi } from 'vitest'

import type { ApiKey, ApiKeyFormData, ApiResponse } from '../../types'
import {
  batchKeyName,
  createApiKeys,
  REVEAL_BATCH_SIZE,
  revealApiKeys,
} from '../create-api-keys'

const payload: ApiKeyFormData = {
  name: 'batch',
  remain_quota: 0,
  expired_time: -1,
  unlimited_quota: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  group: '',
  cross_group_retry: false,
}

function created(id: number, name: string): ApiResponse<ApiKey> {
  return { success: true, data: { id, name } as ApiKey }
}

describe('batchKeyName', () => {
  test('keeps the name for the first key and adds a suffix to the others', () => {
    expect(batchKeyName('batch', 0, () => 'abc')).toBe('batch')
    expect(batchKeyName('batch', 1, () => 'abc')).toBe('batch-abc')
  })

  test('falls back to a default name when none was given', () => {
    expect(batchKeyName('', 0, () => 'abc')).toBe('default-abc')
  })
})

describe('createApiKeys', () => {
  test('creates the requested number of keys in order', async () => {
    let nextId = 10
    const create = vi.fn(async (data: ApiKeyFormData) =>
      created(nextId++, data.name)
    )

    const result = await createApiKeys({
      count: 3,
      payload,
      create,
      suffix: () => 'xyz',
    })

    expect(result.created).toEqual([
      { id: 10, name: 'batch' },
      { id: 11, name: 'batch-xyz' },
      { id: 12, name: 'batch-xyz' },
    ])
    expect(result.stopped).toBe(false)
    expect(create).toHaveBeenCalledTimes(3)
  })

  test('stops at the first rejected response and keeps the keys created before it', async () => {
    const failure: ApiResponse<ApiKey> = { success: false, message: 'limit' }
    const create = vi
      .fn<(data: ApiKeyFormData) => Promise<ApiResponse<ApiKey>>>()
      .mockResolvedValueOnce(created(1, 'batch'))
      .mockResolvedValueOnce(failure)

    const result = await createApiKeys({ count: 5, payload, create })

    expect(result.created).toEqual([{ id: 1, name: 'batch' }])
    expect(result.stopped).toBe(true)
    expect(result.failure).toBe(failure)
    expect(create).toHaveBeenCalledTimes(2)
  })

  test('reports a thrown error as the failure', async () => {
    const error = new Error('network down')
    const create = vi.fn().mockRejectedValue(error)

    const result = await createApiKeys({ count: 2, payload, create })

    expect(result.created).toEqual([])
    expect(result.failure).toBe(error)
  })
})

describe('revealApiKeys', () => {
  test('reveals the keys in batches the backend accepts', async () => {
    const ids = Array.from({ length: 250 }, (_, index) => index + 1)
    const resolveBatch = vi.fn(async (batch: number[]) =>
      Object.fromEntries(batch.map((id) => [id, `sk-${id}`]))
    )

    const keys = await revealApiKeys(ids, resolveBatch)

    expect(resolveBatch.mock.calls.map(([batch]) => batch.length)).toEqual([
      REVEAL_BATCH_SIZE,
      REVEAL_BATCH_SIZE,
      50,
    ])
    expect(Object.keys(keys)).toHaveLength(250)
    expect(keys[250]).toBe('sk-250')
  })

  test('returns what could be revealed when a batch fails', async () => {
    const resolveBatch = vi
      .fn<(batch: number[]) => Promise<Record<number, string>>>()
      .mockResolvedValueOnce({ 1: 'sk-1' })
      .mockResolvedValueOnce({})
    const ids = Array.from({ length: REVEAL_BATCH_SIZE + 1 }, (_, i) => i + 1)

    expect(await revealApiKeys(ids, resolveBatch)).toEqual({ 1: 'sk-1' })
  })
})
