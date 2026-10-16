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
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import { apiKeySchema } from '../../types'
import {
  API_KEY_FORM_DEFAULT_VALUES,
  getApiKeyFormSchema,
  MAX_BATCH_KEYS,
  newApiKeyRoutingSettings,
  storedApiKeyRoutingSettings,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const t = ((key: string, options?: Record<string, unknown>) =>
  key.replace('{{max}}', String(options?.max))) as TFunction

const routing = newApiKeyRoutingSettings(false)

describe('routing settings of the wizard', () => {
  test('a new key uses Auto routing with cross-group retry when the site defaults to it', () => {
    expect(newApiKeyRoutingSettings(true)).toEqual({
      group: 'auto',
      cross_group_retry: true,
      model_limits_enabled: false,
      model_limits: '',
    })
  })

  test('a new key uses the account group and no model whitelist otherwise', () => {
    expect(newApiKeyRoutingSettings(false)).toEqual({
      group: '',
      cross_group_retry: false,
      model_limits_enabled: false,
      model_limits: '',
    })
  })

  test('an edited key keeps the group and model limits it already has', () => {
    const apiKey = apiKeySchema.parse({
      id: 1,
      name: 'old',
      key: 'abc',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'vip',
      cross_group_retry: 1,
      model_limits_enabled: true,
      model_limits: 'gpt-4o,gpt-5',
    })

    expect(storedApiKeyRoutingSettings(apiKey)).toEqual({
      group: 'vip',
      cross_group_retry: true,
      model_limits_enabled: true,
      model_limits: 'gpt-4o,gpt-5',
    })
  })
})

describe('transformFormDataToPayload', () => {
  test('sends an unlimited key without expiration as zero quota and never expires', () => {
    const payload = transformFormDataToPayload(
      { ...API_KEY_FORM_DEFAULT_VALUES, name: 'unlimited' },
      { routing }
    )

    expect(payload).toMatchObject({
      name: 'unlimited',
      remain_quota: 0,
      expired_time: -1,
      unlimited_quota: true,
      group: '',
    })
  })

  test('converts the expiration time to seconds', () => {
    const expires = new Date('2030-01-02T03:04:05Z')

    const payload = transformFormDataToPayload(
      { ...API_KEY_FORM_DEFAULT_VALUES, name: 'dated', expired_time: expires },
      { routing }
    )

    expect(payload.expired_time).toBe(Math.floor(expires.getTime() / 1000))
  })

  test('leaves settings that were not changed out of the request', () => {
    const payload = transformFormDataToPayload(
      { ...API_KEY_FORM_DEFAULT_VALUES, name: 'plain' },
      { routing }
    )

    // Undefined fields are dropped by JSON, so they never reach the server.
    expect(payload.model_families).toBeUndefined()
    expect(payload.service_tier).toBeUndefined()
    expect(payload).not.toHaveProperty('auto_groups')
  })

  test('sends an empty family list and a null tier to clear what a key has', () => {
    const payload = transformFormDataToPayload(
      { ...API_KEY_FORM_DEFAULT_VALUES, name: 'reset' },
      { routing, modelFamilies: [], serviceTier: null }
    )

    expect(payload.model_families).toEqual([])
    expect(payload.service_tier).toBeNull()
  })
})

describe('transformApiKeyToFormDefaults', () => {
  test('fills the form from the stored key without batch creation', () => {
    const apiKey = apiKeySchema.parse({
      id: 7,
      name: 'stored',
      key: 'abc',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: 1893456000,
      created_time: 1,
      accessed_time: 1,
      model_limits_enabled: false,
      allow_ips: '10.0.0.1',
    })

    const values = transformApiKeyToFormDefaults(apiKey)

    expect(values).toMatchObject({
      name: 'stored',
      unlimited_quota: true,
      allow_ips: '10.0.0.1',
      tokenCount: 1,
    })
    expect(values.expired_time?.getTime()).toBe(1893456000 * 1000)
  })
})

describe('getApiKeyFormSchema', () => {
  const schema = getApiKeyFormSchema(t)
  const valid = { ...API_KEY_FORM_DEFAULT_VALUES, name: 'ok' }

  test('requires a name', () => {
    const result = schema.safeParse({ ...valid, name: '' })

    expect(result.error?.issues[0]?.path).toEqual(['name'])
  })

  test('limits how many keys can be created at once', () => {
    expect(
      schema.safeParse({ ...valid, tokenCount: MAX_BATCH_KEYS }).success
    ).toBe(true)
    const result = schema.safeParse({
      ...valid,
      tokenCount: MAX_BATCH_KEYS + 1,
    })

    expect(result.error?.issues[0]?.path).toEqual(['tokenCount'])
  })

  test('requires a non-negative quota unless the key is unlimited', () => {
    const limited = { ...valid, unlimited_quota: false }

    expect(
      schema.safeParse({ ...limited, remain_quota_dollars: -1 }).error
        ?.issues[0]?.path
    ).toEqual(['remain_quota_dollars'])
    expect(
      schema.safeParse({ ...limited, remain_quota_dollars: 5 }).success
    ).toBe(true)
    expect(
      schema.safeParse({ ...valid, remain_quota_dollars: undefined }).success
    ).toBe(true)
  })
})
