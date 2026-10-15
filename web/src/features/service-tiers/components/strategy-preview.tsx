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
import { AlertTriangle, ArrowRight, Info } from 'lucide-react'
import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import { formatTierRatio, tierLabel } from '../lib/tiers'
import type { ServiceTierPreview, ServiceTierStep } from '../types'
import { TierIcon } from './tier-icon'

function TierChain(props: {
  steps: ServiceTierStep[]
  showRatio?: boolean
  locale?: string
}) {
  const { t } = useTranslation()
  return (
    <ol
      className='flex flex-wrap items-center gap-1.5'
      aria-label={t('Tier order')}
    >
      {props.steps.map((step, index) => (
        <Fragment key={step.tier}>
          {index > 0 && (
            <ArrowRight
              className='text-muted-foreground size-3.5 shrink-0'
              aria-hidden='true'
            />
          )}
          <li
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs',
              step.escalation
                ? 'border-dashed'
                : 'bg-primary/5 border-primary/30'
            )}
          >
            <TierIcon tier={step.tier} className='size-3.5' />
            <span className='font-medium'>{tierLabel(t, step.tier)}</span>
            {props.showRatio && (
              <span className='text-muted-foreground font-mono'>
                {formatTierRatio(step.ratio, props.locale)}
              </span>
            )}
            <span className='sr-only'>
              {step.escalation ? t('Escalation') : t('Preferred')}
            </span>
          </li>
        </Fragment>
      ))}
    </ol>
  )
}

type StrategyPreviewProps = {
  preview?: ServiceTierPreview
  loading?: boolean
  problem?: string | null
}

export function StrategyPreview(props: StrategyPreviewProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  if (props.problem) {
    return (
      <p
        role='alert'
        className='text-destructive flex items-start gap-2 text-sm'
      >
        <AlertTriangle className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
        {t(props.problem)}
      </p>
    )
  }
  if (props.loading && !props.preview) {
    return (
      <div className='space-y-3' role='status' aria-label={t('Loading...')}>
        <Skeleton className='h-7 w-2/3' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  }
  if (!props.preview) return null
  const chain = props.preview.chain
  return (
    <div
      className={cn(
        'space-y-4 transition-opacity',
        props.loading && 'opacity-60'
      )}
      aria-busy={props.loading || undefined}
    >
      <div className='space-y-2'>
        <TierChain steps={chain} />
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {chain.length > 1
            ? t(
                'Starts at the first tier and moves right only after every channel of the current tier fails.'
              )
            : t(
                'Only the preferred tier is used, with failover between its own channels.'
              )}
        </p>
      </div>
      {props.preview.families.length > 0 && <Separator />}
      <ul className='space-y-3' aria-label={t('Model families')}>
        {props.preview.families.map((family) => (
          <li key={family.family} className='space-y-2'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <span className='text-sm font-medium'>{family.family}</span>
              {family.steps.length > 0 && (
                <Badge variant='outline' className='font-mono'>
                  {t('Up to {{ratio}}', {
                    ratio: formatTierRatio(family.ceiling_ratio, locale),
                  })}
                </Badge>
              )}
            </div>
            {family.steps.length > 0 ? (
              <TierChain steps={family.steps} showRatio locale={locale} />
            ) : (
              <p className='text-warning flex items-start gap-1.5 text-xs'>
                <AlertTriangle
                  className='mt-0.5 size-3.5 shrink-0'
                  aria-hidden='true'
                />
                {t(
                  'No tier of this family matches these settings; its requests will fail'
                )}
              </p>
            )}
            {family.steps.length > 0 && !family.preferred_offered && (
              <p className='text-muted-foreground flex items-start gap-1.5 text-xs'>
                <Info className='mt-0.5 size-3.5 shrink-0' aria-hidden='true' />
                {t(
                  'This family does not offer {{tier}}; requests start at the next allowed tier',
                  {
                    tier: tierLabel(t, family.preferred_tier),
                  }
                )}
              </p>
            )}
            {family.model_overrides > 0 && (
              <p className='text-muted-foreground text-xs'>
                {t(
                  '{{count}} models in this family have their own tiers or prices',
                  {
                    count: family.model_overrides,
                  }
                )}
              </p>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
