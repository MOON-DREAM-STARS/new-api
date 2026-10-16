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
import { Card, CardContent } from '@/components/ui/card'
import { TierIcon } from '@/features/service-tiers/components/tier-icon'
import { tierLabel } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierKey } from '@/features/service-tiers/types'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import {
  formatMoney,
  formatMultiple,
  priceMultiple,
  type ModelPriceView,
  type ShownPrice,
} from '../lib/prices'
import type { PriceBasis, PriceCurrency, PriceFormat } from '../types'

export interface PriceFormatting {
  basis: PriceBasis
  currency: PriceCurrency
  format: PriceFormat
  /** Yuan per dollar, from the exchange rate of the site. */
  exchangeRate: number
}

interface ModelPriceCardProps {
  view: ModelPriceView
  /** The baseline price; without one the cards show prices only. */
  baseline: ShownPrice | null
  formatting: PriceFormatting
  isBaseline?: boolean
}

function TierBadge(props: { tier?: string }) {
  const { t } = useTranslation()
  if (!props.tier) return <Badge variant='outline'>{t('Standard price')}</Badge>
  return (
    <Badge variant='secondary'>
      <TierIcon tier={props.tier as ServiceTierKey} />
      {tierLabel(t, props.tier)}
    </Badge>
  )
}

/** One model: how many times the baseline price it is, then the price itself. */
export function ModelPriceCard(props: ModelPriceCardProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formatting = props.formatting
  const shown = props.view.shown
  const alternate = props.view.alternate
  const money = (usd: number) =>
    formatMoney(usd, formatting.currency, formatting.exchangeRate, locale)
  const multiple = priceMultiple(shown, props.baseline)
  const modelName = props.view.model.model_name

  return (
    <Card size='sm' role='group' aria-label={modelName}>
      <CardContent className='flex flex-col gap-3'>
        <div className='flex items-start justify-between gap-2'>
          <span className='min-w-0 font-mono text-sm font-medium break-all'>
            {modelName}
          </span>
          {props.isBaseline ? (
            <Badge>{t('Baseline')}</Badge>
          ) : (
            <TierBadge tier={props.view.model.tier} />
          )}
        </div>

        <div className='flex flex-wrap items-baseline gap-x-3 gap-y-1'>
          {multiple !== null && (
            <span
              className={cn(
                'text-3xl font-semibold tracking-tight tabular-nums',
                multiple <= 1 && 'text-primary'
              )}
            >
              {formatMultiple(multiple, formatting.format, locale)}
            </span>
          )}
          <span className='text-muted-foreground text-sm tabular-nums'>
            {t('{{price}} per 1M tokens', { price: money(shown.blended) })}
          </span>
        </div>

        <dl className='text-muted-foreground grid grid-cols-2 gap-2 border-t pt-3 text-xs'>
          <div className='flex flex-col gap-0.5'>
            <dt>{t('Input')}</dt>
            <dd className='text-foreground tabular-nums'>
              {money(shown.input)}
            </dd>
          </div>
          <div className='flex flex-col gap-0.5'>
            <dt>{t('Output')}</dt>
            <dd className='text-foreground tabular-nums'>
              {money(shown.output)}
            </dd>
          </div>
        </dl>

        {alternate && (
          <p className='text-muted-foreground text-xs tabular-nums'>
            {formatting.basis === 'peak'
              ? t('Off-peak: {{price}} per 1M tokens', {
                  price: money(alternate.blended),
                })
              : t('Peak: {{price}} per 1M tokens', {
                  price: money(alternate.blended),
                })}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
