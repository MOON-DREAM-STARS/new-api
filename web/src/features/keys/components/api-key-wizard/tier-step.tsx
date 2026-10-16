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
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ServiceTierSettingsEditor } from '@/features/service-tiers/components/service-tier-settings-editor'
import { tierRatioRanges } from '@/features/service-tiers/lib/tiers'
import type {
  ServiceTierSettings,
  ServiceTierUserOverview,
} from '@/features/service-tiers/types'

import {
  displayedTierSettings,
  resultingTierOverride,
  type KeyTierChoice,
} from '../../lib/key-service-tier'

type TierStepProps = {
  /** The account's service tier settings, with what each tier costs. */
  overview: ServiceTierUserOverview | undefined
  loading: boolean
  errorMessage?: string
  /** The key's own settings; null while it follows the account default. */
  own: ServiceTierSettings | null
  choice: KeyTierChoice | null
  disabled?: boolean
  onChoiceChange: (choice: KeyTierChoice | null) => void
  onRetry: () => void
}

/**
 * Second page of the wizard: the service tiers of this key. It starts from the
 * account default and stores nothing unless the user changes a setting.
 */
export function TierStep(props: TierStepProps) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      <div role='status' aria-label={t('Loading...')} className='space-y-4'>
        <div className='grid gap-3 sm:grid-cols-3'>
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className='h-36 w-full' />
          ))}
        </div>
        <Skeleton className='h-28 w-full' />
      </div>
    )
  }
  if (!props.overview) {
    return (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load service tiers')}
        description={props.errorMessage}
        onRetry={props.onRetry}
      />
    )
  }

  const account = props.overview.settings
  const displayed = displayedTierSettings(props.choice, props.own, account)
  const hasOwnSettings =
    resultingTierOverride(props.choice, props.own, account) !== null
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3'>
        <div className='min-w-0 flex-1 basis-56 space-y-0.5'>
          <p className='text-sm font-medium'>
            {hasOwnSettings
              ? t('Custom tiers for this API key')
              : t('Following your account default')}
          </p>
          <p className='text-muted-foreground text-xs leading-relaxed'>
            {hasOwnSettings
              ? t(
                  'This API key uses the tiers below instead of the account default.'
                )
              : t(
                  'Change a setting below only if this API key should use different tiers.'
                )}
          </p>
        </div>
        {hasOwnSettings && (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => props.onChoiceChange({ kind: 'follow' })}
          >
            {t('Follow account default')}
          </Button>
        )}
      </div>
      {!props.overview.available && (
        <Alert>
          <Info className='size-4' aria-hidden='true' />
          <AlertTitle>{t('No model uses service tiers yet')}</AlertTitle>
          <AlertDescription>
            {t(
              'Your choices are saved and apply once the administrator turns on service tiers for a model family.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <ServiceTierSettingsEditor
        scope='key'
        settings={displayed}
        ratios={tierRatioRanges(props.overview)}
        disabled={props.disabled}
        onChange={(settings) =>
          props.onChoiceChange({ kind: 'custom', settings })
        }
      />
    </div>
  )
}
