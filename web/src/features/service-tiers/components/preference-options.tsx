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
import { useTranslation } from 'react-i18next'

import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn } from '@/lib/utils'

import {
  preferenceDescription,
  preferenceLabel,
  preferenceStartTier,
  SERVICE_TIER_PREFERENCES,
} from '../lib/tiers'
import type { ServiceTierPreference, ServiceTierSettings } from '../types'

type PreferenceOptionsProps = {
  settings: ServiceTierSettings
  disabled?: boolean
  invalid?: boolean
  onChange: (preference: ServiceTierPreference) => void
}

export function PreferenceOptions(props: PreferenceOptionsProps) {
  const { t } = useTranslation()
  return (
    <RadioGroup
      aria-label={t('Default preference')}
      aria-invalid={props.invalid || undefined}
      value={props.settings.preference}
      onValueChange={(value) => {
        if (SERVICE_TIER_PREFERENCES.includes(value as ServiceTierPreference)) {
          props.onChange(value as ServiceTierPreference)
        }
      }}
      className='gap-2 sm:grid-cols-3'
    >
      {SERVICE_TIER_PREFERENCES.map((preference) => {
        const start = preferenceStartTier({ ...props.settings, preference })
        const unavailable =
          !start || !props.settings.enabled_tiers.includes(start)
        const checked = props.settings.preference === preference
        const id = `service-tier-preference-${preference}`
        return (
          <div
            key={preference}
            className={cn(
              'rounded-lg border transition-colors',
              checked && 'border-primary/50 bg-primary/[0.03]',
              checked && unavailable && 'border-destructive/60'
            )}
          >
            <Label
              htmlFor={id}
              className={cn(
                'hover:bg-muted/30 flex h-full cursor-pointer items-start gap-3 rounded-lg px-4 py-3 font-normal',
                (props.disabled || (unavailable && !checked)) &&
                  'cursor-not-allowed opacity-60'
              )}
            >
              <RadioGroupItem
                id={id}
                value={preference}
                className='mt-0.5'
                disabled={props.disabled || (unavailable && !checked)}
                aria-describedby={`${id}-description`}
              />
              <span className='flex min-w-0 flex-col gap-1'>
                <span className='text-sm font-medium'>
                  {preferenceLabel(t, preference)}
                </span>
                <span
                  id={`${id}-description`}
                  className='text-muted-foreground text-xs'
                >
                  {unavailable
                    ? t('Turn on this tier to use this preference')
                    : preferenceDescription(t, preference)}
                </span>
              </span>
            </Label>
          </div>
        )
      })}
    </RadioGroup>
  )
}
