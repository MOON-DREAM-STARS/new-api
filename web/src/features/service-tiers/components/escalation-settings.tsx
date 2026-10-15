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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'

import {
  escalationCeilingOptions,
  formatTierRatio,
  tierLabel,
} from '../lib/tiers'
import type { ServiceTierKey, ServiceTierSettings } from '../types'
import type { TierRatioRange } from './tier-switch-cards'

type EscalationSettingsProps = {
  settings: ServiceTierSettings
  ratios: TierRatioRange
  disabled?: boolean
  onAllowChange: (allow: boolean) => void
  onCeilingChange: (tier: ServiceTierKey) => void
}

export function EscalationSettings(props: EscalationSettingsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const options = escalationCeilingOptions(props.settings)
  const items = options.map((tier) => {
    const range = props.ratios[tier]
    const ratio = range
      ? formatTierRatio(range.max, locale)
      : t('Not offered yet')
    return { value: tier, label: tierLabel(t, tier), ratio }
  })
  const noHigherTier = options.length === 0
  const selected = items.find(
    (item) => item.value === props.settings.max_escalation_tier
  )
  return (
    <div className='space-y-4'>
      <div className='flex items-start justify-between gap-4'>
        <div className='min-w-0 space-y-1'>
          <Label
            htmlFor='service-tier-escalation'
            className='text-sm font-medium'
          >
            {t('Allow escalation to higher tiers')}
          </Label>
          <p
            id='service-tier-escalation-description'
            className='text-muted-foreground text-xs leading-relaxed'
          >
            {t(
              'When every channel of the current tier is unavailable, the request may try a higher tier you turned on, within the highest tier below. It never moves to a cheaper tier.'
            )}
          </p>
        </div>
        <Switch
          id='service-tier-escalation'
          checked={props.settings.allow_escalation}
          disabled={props.disabled || noHigherTier}
          aria-describedby='service-tier-escalation-description'
          onCheckedChange={props.onAllowChange}
        />
      </div>
      <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,16rem)] sm:items-center'>
        <div className='min-w-0'>
          <Label htmlFor='service-tier-ceiling' className='text-sm'>
            {t('Highest escalation tier')}
          </Label>
          <p className='text-muted-foreground mt-1 text-xs'>
            {noHigherTier
              ? t('No higher tier is turned on above your preferred tier')
              : t(
                  'The ratio shown is the highest across model families; models may differ'
                )}
          </p>
        </div>
        <Select
          items={items}
          value={props.settings.max_escalation_tier || null}
          disabled={
            props.disabled || !props.settings.allow_escalation || noHigherTier
          }
          onValueChange={(value) => {
            if (value) props.onCeilingChange(value as ServiceTierKey)
          }}
        >
          <SelectTrigger id='service-tier-ceiling' className='w-full'>
            <SelectValue className='min-w-0'>
              <span className='truncate'>
                {selected
                  ? `${selected.label} · ${selected.ratio}`
                  : t('Not set')}
              </span>
            </SelectValue>
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  <span className='flex w-full items-center justify-between gap-3'>
                    <span>{item.label}</span>
                    <span className='text-muted-foreground font-mono text-xs'>
                      {item.ratio}
                    </span>
                  </span>
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}
