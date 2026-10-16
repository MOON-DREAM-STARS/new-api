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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getPriceComparisonAdmin,
  priceComparisonAdminQueryKey,
  savePriceComparisonSettings,
} from '../api'
import { isComparable } from '../lib/prices'
import { picksOf, sameSettings, withPicks } from '../lib/settings'
import type {
  PriceBasis,
  PriceComparisonAdminModel,
  PriceComparisonSettings,
  PriceCurrency,
  PriceFormat,
} from '../types'
import { FamilyPicker, MAX_MODELS_PER_FAMILY } from './family-picker'

interface ChoiceFieldProps<T extends string> {
  label: string
  description: ReactNode
  value: T
  options: readonly { value: T; label: string }[]
  disabled?: boolean
  onChange: (value: T) => void
}

/** A setting with a few mutually exclusive choices. */
function ChoiceField<T extends string>(props: ChoiceFieldProps<T>) {
  const labelId = useId()
  return (
    <Field>
      <FieldLabel id={labelId}>{props.label}</FieldLabel>
      <ToggleGroup
        value={[props.value]}
        onValueChange={(values) => {
          const next = props.options.find(
            (option) => option.value === values[0]
          )
          if (next) props.onChange(next.value)
        }}
        variant='outline'
        size='sm'
        disabled={props.disabled}
        aria-labelledby={labelId}
      >
        {props.options.map((option) => (
          <ToggleGroupItem key={option.value} value={option.value}>
            {option.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <FieldDescription>{props.description}</FieldDescription>
    </Field>
  )
}

interface SettingsDialogBodyProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function SettingsDialogBody(props: SettingsDialogBodyProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const baselineId = useId()
  const admin = useQuery({
    queryKey: priceComparisonAdminQueryKey,
    queryFn: getPriceComparisonAdmin,
    enabled: props.open,
    staleTime: 0,
  })
  const [draft, setDraft] = useState<PriceComparisonSettings | null>(null)
  const saved = admin.data?.settings
  const current = draft ?? saved ?? null
  const dirty = Boolean(current && saved && !sameSettings(current, saved))

  const save = useMutation({
    mutationFn: savePriceComparisonSettings,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['price-comparison'] })
      toast.success(t('Price comparison settings saved'))
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error, t('Failed to save')),
  })

  const models = useMemo(
    () =>
      new Map<string, PriceComparisonAdminModel>(
        (admin.data?.models ?? []).map((model) => [model.model_name, model])
      ),
    [admin.data?.models]
  )
  const baselineOptions = useMemo(
    () =>
      (admin.data?.models ?? [])
        .filter((model) => isComparable(model.price))
        .map((model) => ({ value: model.model_name, label: model.model_name })),
    [admin.data?.models]
  )

  const update = (patch: Partial<PriceComparisonSettings>) => {
    if (current) setDraft({ ...current, ...patch })
  }

  let content: ReactNode
  if (admin.isLoading) {
    content = (
      <div
        role='status'
        aria-label={t('Loading...')}
        className='flex flex-col gap-4'
      >
        <Skeleton className='h-9 w-full' />
        <Skeleton className='h-9 w-2/3' />
        <Skeleton className='h-32 w-full' />
      </div>
    )
  } else if (!admin.data || !current) {
    content = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Failed to load the settings')}</AlertTitle>
        <AlertDescription>
          {admin.error
            ? getServerErrorMessage(admin.error)
            : t('Refresh the list and try again.')}
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='mt-2'
            onClick={() => void admin.refetch()}
          >
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else {
    const rate = admin.data.usd_exchange_rate
    content = (
      <div className='flex flex-col gap-6'>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor={baselineId}>{t('Baseline model')}</FieldLabel>
            <Combobox
              id={baselineId}
              options={baselineOptions}
              value={current.baseline_model}
              placeholder={t('Search models')}
              disabled={save.isPending}
              onValueChange={(name) => name && update({ baseline_model: name })}
            />
            <FieldDescription>
              {t(
                'Every multiple compares with the list price of this model, without any tier or group discount.'
              )}
            </FieldDescription>
          </Field>
          <ChoiceField<PriceBasis>
            label={t('Price period')}
            description={t(
              'Models that charge by time of day use this period. Other models have one price.'
            )}
            value={current.price_basis}
            disabled={save.isPending}
            options={[
              { value: 'peak', label: t('Peak price') },
              { value: 'off_peak', label: t('Off-peak price') },
            ]}
            onChange={(price_basis) => update({ price_basis })}
          />
          <ChoiceField<PriceCurrency>
            label={t('Currency')}
            description={
              current.currency === 'CNY'
                ? t(
                    'Yuan prices use the exchange rate of this site: 1 USD = {{rate}} CNY.',
                    { rate }
                  )
                : t('Prices are shown in US dollars.')
            }
            value={current.currency}
            disabled={save.isPending}
            options={[
              { value: 'USD', label: t('US dollar') },
              { value: 'CNY', label: t('Chinese yuan') },
            ]}
            onChange={(currency) => update({ currency })}
          />
          <ChoiceField<PriceFormat>
            label={t('Comparison format')}
            description={t('How a price compares with the baseline model.')}
            value={current.ratio_format}
            disabled={save.isPending}
            options={[
              { value: 'multiplier', label: t('Multiple (0.40X)') },
              { value: 'percent', label: t('Percent (40%)') },
            ]}
            onChange={(ratio_format) => update({ ratio_format })}
          />
        </FieldGroup>

        <div className='flex flex-col gap-3'>
          <div className='flex flex-col gap-0.5'>
            <h3 className='text-sm font-semibold'>
              {t('Models by model family')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Choose up to {{max}} models for each model family. Users only see the models they can use.',
                { max: MAX_MODELS_PER_FAMILY }
              )}
            </p>
          </div>
          {admin.data.families.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('There are no model families yet.')}
            </p>
          )}
          {admin.data.families.map((family) => (
            <FamilyPicker
              key={family.id}
              family={family}
              models={models}
              members={admin.data.models.filter(
                (model) => model.family_id === family.id
              )}
              selected={picksOf(current, family.id)}
              disabled={save.isPending}
              onChange={(names) =>
                setDraft(withPicks(current, family.id, names))
              }
            />
          ))}
        </div>
      </div>
    )
  }

  const canSave = Boolean(current?.baseline_model) && dirty && !save.isPending

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => !save.isPending && props.onOpenChange(open)}
      title={t('Price comparison settings')}
      description={t(
        'Choose the baseline, how prices are shown and which models each model family lists.'
      )}
      contentHeight='min(64dvh, 640px)'
      contentClassName='sm:max-w-2xl'
      showCloseButton={!save.isPending}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={save.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={!canSave}
            onClick={() => current && save.mutate(current)}
          >
            {save.isPending && <Spinner data-icon='inline-start' />}
            {save.isPending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      {content}
    </Dialog>
  )
}

/** The administrator settings of the price comparison page. */
export function PriceSettingsDialog(props: SettingsDialogBodyProps) {
  // Every opening starts from the saved settings.
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(props.open)
  if (props.open !== wasOpen) {
    setWasOpen(props.open)
    if (props.open) setSession((value) => value + 1)
  }
  return (
    <SettingsDialogBody
      key={session}
      open={props.open}
      onOpenChange={props.onOpenChange}
    />
  )
}
