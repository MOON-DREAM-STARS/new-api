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
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import { DEFAULT_GROUP } from '../constants'
import type { ApiKey, ApiKeyFormData, ApiKeyServiceTierInput } from '../types'

/** Keys created in one go; the keys are revealed in batches of 100. */
export const MAX_BATCH_KEYS = 100

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(t: TFunction) {
  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().nullable(),
      unlimited_quota: z.boolean(),
      allow_ips: z.string().optional(),
      tokenCount: z
        .number()
        .min(1)
        .max(
          MAX_BATCH_KEYS,
          t('You can create at most {{max}} API keys at once', {
            max: MAX_BATCH_KEYS,
          })
        )
        .optional(),
      allow_user_quota_query: z.boolean().optional(),
    })
    .superRefine((data, ctx) => {
      if (data.unlimited_quota) {
        return
      }

      if (
        data.remain_quota_dollars === undefined ||
        data.remain_quota_dollars < 0
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['remain_quota_dollars'],
          message: t('Quota must be zero or greater'),
        })
      }
    })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: null,
  unlimited_quota: true,
  allow_ips: '',
  allow_user_quota_query: true,
  tokenCount: 1,
}

// ============================================================================
// Routing settings the wizard does not show
// ============================================================================

/**
 * The group and model limit of a key. The wizard no longer asks for them: a
 * new key follows the site default and an edited key keeps what it has.
 */
export type ApiKeyRoutingSettings = Pick<
  ApiKeyFormData,
  'group' | 'cross_group_retry' | 'model_limits_enabled' | 'model_limits'
>

/** A new key uses Auto routing when the site defaults to it, else the account group. */
export function newApiKeyRoutingSettings(
  useAutoGroup: boolean
): ApiKeyRoutingSettings {
  return {
    group: useAutoGroup ? 'auto' : DEFAULT_GROUP,
    cross_group_retry: useAutoGroup,
    model_limits_enabled: false,
    model_limits: '',
  }
}

export function storedApiKeyRoutingSettings(
  apiKey: ApiKey
): ApiKeyRoutingSettings {
  return {
    group: apiKey.group || DEFAULT_GROUP,
    cross_group_retry: !!apiKey.cross_group_retry,
    model_limits_enabled: apiKey.model_limits_enabled,
    model_limits: apiKey.model_limits ?? '',
  }
}

// ============================================================================
// Form Data Transformation
// ============================================================================

type ApiKeyPayloadParts = {
  routing: ApiKeyRoutingSettings
  /** Undefined keeps the stored limit; an empty list removes it. */
  modelFamilies?: number[]
  /** Undefined keeps the stored settings; null follows the account default. */
  serviceTier?: ApiKeyServiceTierInput | null
}

/**
 * Transform form data to API payload. Auto groups are never sent, so a new
 * key follows the global Auto order and an edited key keeps its own.
 */
export function transformFormDataToPayload(
  data: ApiKeyFormValues,
  parts: ApiKeyPayloadParts
): ApiKeyFormData {
  return {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    allow_ips: data.allow_ips || '',
    ...parts.routing,
    model_families: parts.modelFamilies,
    service_tier: parts.serviceTier,
    allow_user_quota_query: data.allow_user_quota_query ?? true,
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey
): ApiKeyFormValues {
  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0 ? new Date(apiKey.expired_time * 1000) : null,
    unlimited_quota: apiKey.unlimited_quota,
    allow_ips: apiKey.allow_ips || '',
    allow_user_quota_query: !!apiKey.allow_user_quota_query,
    tokenCount: 1,
  }
}
