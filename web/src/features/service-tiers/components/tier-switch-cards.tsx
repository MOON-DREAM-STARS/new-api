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

import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import {
  formatTierRatio,
  SERVICE_TIERS,
  tierDescription,
  tierLabel,
} from '../lib/tiers'
import type { ServiceTierKey, TierRatioRange } from '../types'
import { TierIcon } from './tier-icon'

type TierSwitchCardsProps = {
  enabledTiers: ServiceTierKey[]
  ratios: TierRatioRange
  disabled?: boolean
  onToggle: (tier: ServiceTierKey, enabled: boolean) => void
}

export function TierSwitchCards(props: TierSwitchCardsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <div className='grid gap-3 sm:grid-cols-3'>
      {SERVICE_TIERS.map((tier) => {
        const enabled = props.enabledTiers.includes(tier)
        const lastEnabled = enabled && props.enabledTiers.length === 1
        const range = props.ratios[tier]
        const labelId = `service-tier-${tier}-label`
        const descriptionId = `service-tier-${tier}-description`
        let ratioText = t('Not offered yet')
        if (range) {
          ratioText =
            range.min === range.max
              ? formatTierRatio(range.min, locale)
              : `${formatTierRatio(range.min, locale)} – ${formatTierRatio(range.max, locale)}`
        }
        return (
          <div
            key={tier}
            data-tier={tier}
            data-enabled={enabled}
            className={cn(
              'bg-card flex min-w-0 flex-col gap-3 rounded-xl border p-4 transition-colors',
              enabled ? 'border-primary/40 bg-primary/[0.03]' : 'opacity-90'
            )}
          >
            <div className='flex items-start justify-between gap-3'>
              <div className='flex min-w-0 items-center gap-2.5'>
                <span
                  className={cn(
                    'flex size-8 shrink-0 items-center justify-center rounded-lg border',
                    enabled
                      ? 'bg-primary/10 text-primary border-primary/20'
                      : 'bg-muted text-muted-foreground'
                  )}
                >
                  <TierIcon tier={tier} className='size-4' />
                </span>
                <span id={labelId} className='truncate text-sm font-semibold'>
                  {tierLabel(t, tier)}
                </span>
              </div>
              <Switch
                checked={enabled}
                disabled={props.disabled || lastEnabled}
                aria-labelledby={labelId}
                aria-describedby={descriptionId}
                onCheckedChange={(checked) => props.onToggle(tier, checked)}
              />
            </div>
            <p
              id={descriptionId}
              className='text-muted-foreground text-xs leading-relaxed'
            >
              {tierDescription(t, tier)}
              {lastEnabled && (
                <span className='mt-1 block'>
                  {t('At least one tier must stay on')}
                </span>
              )}
            </p>
            <div className='mt-auto flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Billing ratio')}
              </span>
              <Badge
                variant={range ? 'secondary' : 'outline'}
                className='font-mono'
              >
                {ratioText}
              </Badge>
            </div>
          </div>
        )
      })}
    </div>
  )
}
