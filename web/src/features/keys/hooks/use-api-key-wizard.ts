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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useForm, type SubmitErrorHandler } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useApiKeys } from '@/features/keys/components/api-keys-provider'
import {
  getServiceTierOverview,
  serviceTierOverviewQueryKey,
} from '@/features/service-tiers/api'
import { draftProblem } from '@/features/service-tiers/lib/tiers'
import { useStatus } from '@/hooks/use-status'
import { getUserGroups } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { getApiKey, updateApiKey } from '../api'
import type { CreatedApiKey } from '../components/api-key-wizard/key-result-step'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  API_KEY_FORM_DEFAULT_VALUES,
  getApiKeyFormSchema,
  newApiKeyRoutingSettings,
  storedApiKeyRoutingSettings,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
  type ApiKeyFormValues,
} from '../lib'
import { createApiKeys, revealApiKeys } from '../lib/create-api-keys'
import {
  displayedTierSettings,
  keyTierPayload,
  type KeyTierChoice,
} from '../lib/key-service-tier'
import {
  familyLimitFromSelection,
  initialFamilySelection,
  sameFamilyLimit,
  selectableFamilyIds,
} from '../lib/model-families'
import type { ApiKey, ApiKeyServiceTierInput } from '../types'
import { useUserModelFamilies } from './use-user-model-families'

/** The pages of the wizard; the key page only exists when creating. */
export const WIZARD_STEP = {
  families: 0,
  tier: 1,
  options: 2,
  key: 3,
} as const

type WizardParams = {
  open: boolean
  currentRow?: ApiKey
  onOpenChange: (open: boolean) => void
}

type CreatedKeys = { keys: CreatedApiKey[]; requested: number }

const NO_IDS: number[] = []

export function useApiKeyWizard(params: WizardParams) {
  const { t } = useTranslation()
  const { triggerRefresh, resolveRealKeysBatch } = useApiKeys()
  const { status, loading: statusLoading } = useStatus()
  const isUpdate = params.currentRow !== undefined
  const keyId = params.currentRow?.id

  const [step, setStep] = useState<number>(WIZARD_STEP.families)
  const [familyChoice, setFamilyChoice] = useState<ReadonlySet<number> | null>(
    null
  )
  const [tierChoice, setTierChoice] = useState<KeyTierChoice | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [created, setCreated] = useState<CreatedKeys | null>(null)
  const [revealing, setRevealing] = useState(false)

  const families = useUserModelFamilies(params.open)
  const tierOverview = useQuery({
    queryKey: serviceTierOverviewQueryKey,
    queryFn: getServiceTierOverview,
    enabled: params.open,
    staleTime: 0,
    meta: { errorToast: false },
  })
  const groups = useQuery({
    queryKey: ['user-groups'],
    queryFn: async () => requireServerSuccess(await getUserGroups()),
    enabled: params.open && !isUpdate,
    staleTime: 0,
  })
  const detail = useQuery({
    queryKey: ['api-key', keyId],
    queryFn: async () => requireServerSuccess(await getApiKey(keyId ?? 0)),
    enabled: params.open && isUpdate,
    staleTime: 0,
  })
  const storedKey = detail.data?.data

  const schema = useMemo(() => getApiKeyFormSchema(t), [t])
  const formValues = useMemo(
    () => (storedKey ? transformApiKeyToFormDefaults(storedKey) : undefined),
    [storedKey]
  )
  const form = useForm<ApiKeyFormValues>({
    resolver: zodResolver(schema),
    defaultValues: API_KEY_FORM_DEFAULT_VALUES,
    values: formValues,
    resetOptions: { keepDirtyValues: true },
  })

  // Model families: everything is selected until the user changes it.
  const storedFamilyIds = useMemo(
    () => storedKey?.model_families ?? NO_IDS,
    [storedKey]
  )
  const waitingForKey = isUpdate && !storedKey
  const familyData = waitingForKey ? undefined : families.data
  const selection = useMemo(() => {
    if (familyChoice) return familyChoice
    if (!familyData) return new Set<number>()
    return initialFamilySelection(familyData, storedFamilyIds)
  }, [familyChoice, familyData, storedFamilyIds])
  const selectable = familyData
    ? selectableFamilyIds(familyData, storedFamilyIds)
    : NO_IDS
  const selectedCount = selectable.filter((id) => selection.has(id)).length
  const familiesValid =
    Boolean(familyData) && (selectable.length === 0 || selectedCount > 0)

  // Service tier: the key follows the account until the user changes it.
  const ownTier = storedKey?.service_tier ?? null
  const account = tierOverview.data?.settings
  const shownTier = account
    ? displayedTierSettings(tierChoice, ownTier, account)
    : null
  const tierValid = shownTier !== null && draftProblem(shownTier) === null

  const canOpenStep = (index: number) => {
    if (index <= WIZARD_STEP.families) return true
    if (index === WIZARD_STEP.tier) return familiesValid
    return familiesValid && tierValid
  }

  const buildPayload = (values: ApiKeyFormValues) => {
    const defaultUseAutoGroup =
      status?.default_use_auto_group === true &&
      Object.keys(groups.data?.data ?? {}).includes('auto')
    const routing = storedKey
      ? storedApiKeyRoutingSettings(storedKey)
      : newApiKeyRoutingSettings(defaultUseAutoGroup)
    // Settings left alone are not sent, so the key keeps what it has.
    let modelFamilies: number[] | undefined
    if (familyChoice && familyData) {
      const limit = familyLimitFromSelection(
        familyData,
        storedFamilyIds,
        familyChoice
      )
      if (!sameFamilyLimit(limit, storedFamilyIds)) modelFamilies = limit
    }
    let serviceTier: ApiKeyServiceTierInput | null | undefined
    if (account) {
      const tier = keyTierPayload(tierChoice, ownTier, account)
      if (tier.changed) serviceTier = tier.value
    }
    return transformFormDataToPayload(values, {
      routing,
      modelFamilies,
      serviceTier,
    })
  }

  const reveal = async (keys: CreatedApiKey[]) => {
    setRevealing(true)
    try {
      const revealed = await revealApiKeys(
        keys.map((item) => item.id),
        resolveRealKeysBatch
      )
      setCreated(
        (current) =>
          current && {
            ...current,
            keys: current.keys.map((item) => ({
              ...item,
              key: revealed[item.id] ?? item.key,
            })),
          }
      )
    } finally {
      setRevealing(false)
    }
  }

  const create = async (values: ApiKeyFormValues) => {
    const requested = values.tokenCount || 1
    const outcome = await createApiKeys({
      count: requested,
      payload: buildPayload(values),
    })
    if (outcome.stopped) {
      handleServerError(outcome.failure, t(ERROR_MESSAGES.CREATE_FAILED))
    }
    if (outcome.created.length === 0) return
    const keys = outcome.created.map((item) => ({ ...item, key: null }))
    triggerRefresh()
    setCreated({ keys, requested })
    setStep(WIZARD_STEP.key)
    await reveal(keys)
  }

  const update = async (values: ApiKeyFormValues) => {
    const result = await updateApiKey({
      ...buildPayload(values),
      id: keyId ?? 0,
    })
    if (!result.success) {
      handleServerError(result, t(ERROR_MESSAGES.UPDATE_FAILED))
      return
    }
    toast.success(t(SUCCESS_MESSAGES.API_KEY_UPDATED))
    params.onOpenChange(false)
    triggerRefresh()
  }

  const onSubmit = async (values: ApiKeyFormValues) => {
    setSubmitting(true)
    try {
      if (isUpdate) await update(values)
      else await create(values)
    } catch (error) {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setSubmitting(false)
    }
  }

  const onInvalid: SubmitErrorHandler<ApiKeyFormValues> = () => {
    toast.error(t('Please fix the highlighted fields before saving'))
  }

  const routingReady = isUpdate || (!statusLoading && groups.isFetched)

  return {
    isUpdate,
    step,
    openStep: (index: number) => {
      if (canOpenStep(index)) setStep(index)
    },
    canOpenStep,
    form,
    submit: form.handleSubmit(onSubmit, onInvalid),
    submitting,
    routingReady,
    families: {
      data: familyData,
      loading: families.isPending || (isUpdate && detail.isPending),
      errorMessage: getServerErrorMessage(families.error ?? detail.error),
      storedIds: storedFamilyIds,
      selection,
      valid: familiesValid,
      setSelection: setFamilyChoice,
      retry: () => {
        void families.refetch()
        if (isUpdate) void detail.refetch()
      },
    },
    tier: {
      overview: tierOverview.data,
      loading: tierOverview.isPending || (isUpdate && detail.isPending),
      errorMessage: getServerErrorMessage(tierOverview.error),
      own: ownTier,
      choice: tierChoice,
      valid: tierValid,
      setChoice: setTierChoice,
      retry: () => void tierOverview.refetch(),
    },
    created,
    revealing,
    retryReveal: () => {
      if (created) void reveal(created.keys)
    },
  }
}
