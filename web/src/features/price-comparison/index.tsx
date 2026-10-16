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
import { useQuery } from '@tanstack/react-query'
import { Info, SlidersHorizontal } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getPriceComparison, priceComparisonQueryKey } from './api'
import {
  ModelPriceCard,
  type PriceFormatting,
} from './components/model-price-card'
import { PriceSettingsDialog } from './components/settings-dialog'
import { buildModelPriceView, type ModelPriceView } from './lib/prices'
import type { PriceComparisonView } from './types'

interface FamilyPrices {
  id: number
  name: string
  models: ModelPriceView[]
}

/** Reads every price once; models without a comparable token price are left out. */
function prepare(data: PriceComparisonView) {
  const basis = data.settings.price_basis
  const families: FamilyPrices[] = []
  for (const family of data.families) {
    const models = family.models.flatMap((model) => {
      const priceView = buildModelPriceView(model, basis)
      return priceView ? [priceView] : []
    })
    if (models.length > 0) {
      families.push({ id: family.id, name: family.name, models })
    }
  }
  return { baseline: buildModelPriceView(data.baseline, basis), families }
}

function PriceGrid(props: { children: ReactNode }) {
  return (
    <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
      {props.children}
    </div>
  )
}

function Footnote(props: { formatting: PriceFormatting }) {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground flex flex-col gap-1 px-1 text-xs leading-relaxed'>
      <p className='flex items-start gap-2'>
        <Info className='mt-0.5 size-3.5 shrink-0' aria-hidden='true' />
        {t(
          'Prices are per 1M tokens. A blended price counts 3 input tokens for every output token.'
        )}
      </p>
      <p className='ps-5'>
        {t(
          'Each price is the lowest you can get for that model: the lowest service tier it offers, otherwise the lowest group ratio. Your usage logs show what you are actually charged.'
        )}
      </p>
      {props.formatting.currency === 'CNY' && (
        <p className='ps-5'>
          {t(
            'Yuan prices use the exchange rate of this site: 1 USD = {{rate}} CNY.',
            {
              rate: props.formatting.exchangeRate,
            }
          )}
        </p>
      )}
    </div>
  )
}

function baselineNote(
  t: (key: string) => string,
  baseline: ModelPriceView,
  basis: PriceFormatting['basis']
): string {
  if (!baseline.list.timeBased) {
    return t('Every multiple compares with the list price of this model.')
  }
  return basis === 'peak'
    ? t('Every multiple compares with the peak price of this model.')
    : t('Every multiple compares with the off-peak price of this model.')
}

export function PriceComparison() {
  const { t } = useTranslation()
  const isAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.ADMIN
  )
  const [settingsOpen, setSettingsOpen] = useState(false)
  const view = useQuery({
    queryKey: priceComparisonQueryKey,
    queryFn: getPriceComparison,
  })
  const prepared = useMemo(
    () => (view.data ? prepare(view.data) : null),
    [view.data]
  )

  let content: ReactNode
  if (view.isLoading) {
    content = (
      <div
        role='status'
        aria-label={t('Loading...')}
        className='flex flex-col gap-6'
      >
        <Skeleton className='h-36 w-full sm:max-w-sm' />
        <PriceGrid>
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className='h-36 w-full' />
          ))}
        </PriceGrid>
      </div>
    )
  } else if (!view.data || !prepared) {
    content = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('Failed to load the price comparison')}</EmptyTitle>
          <EmptyDescription>
            {view.error
              ? getServerErrorMessage(view.error)
              : t('Refresh the list and try again.')}
          </EmptyDescription>
        </EmptyHeader>
        <Button
          type='button'
          variant='outline'
          onClick={() => void view.refetch()}
        >
          {t('Retry')}
        </Button>
      </Empty>
    )
  } else {
    const settings = view.data.settings
    const formatting: PriceFormatting = {
      basis: settings.price_basis,
      currency: settings.currency,
      format: settings.ratio_format,
      exchangeRate: view.data.usd_exchange_rate,
    }
    const baseline = prepared.baseline
    content = (
      <div className='flex flex-col gap-6'>
        {!baseline && (
          <Alert>
            <Info className='size-4' aria-hidden='true' />
            <AlertTitle>
              {t('The baseline model cannot be compared')}
            </AlertTitle>
            <AlertDescription>
              {t(
                'Prices are shown without a comparison until it has a token price.'
              )}
            </AlertDescription>
          </Alert>
        )}

        {baseline && (
          <section
            aria-labelledby='price-baseline'
            className='flex flex-col gap-3'
          >
            <div className='flex flex-col gap-0.5'>
              <h3 id='price-baseline' className='text-sm font-semibold'>
                {t('Baseline')}
              </h3>
              <p className='text-muted-foreground text-xs'>
                {baselineNote(t, baseline, settings.price_basis)}
              </p>
            </div>
            <PriceGrid>
              <ModelPriceCard
                view={baseline}
                baseline={baseline.shown}
                formatting={formatting}
                isBaseline
              />
            </PriceGrid>
          </section>
        )}

        {prepared.families.map((family) => (
          <section
            key={family.id}
            aria-labelledby={`price-family-${family.id}`}
            className='flex flex-col gap-3'
          >
            <h3
              id={`price-family-${family.id}`}
              className='text-sm font-semibold'
            >
              {family.name}
            </h3>
            <PriceGrid>
              {family.models.map((item) => (
                <ModelPriceCard
                  key={item.model.model_name}
                  view={item}
                  baseline={baseline?.shown ?? null}
                  formatting={formatting}
                />
              ))}
            </PriceGrid>
          </section>
        ))}

        {prepared.families.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>{t('No prices to compare yet')}</EmptyTitle>
              <EmptyDescription>
                {isAdmin
                  ? t(
                      'Choose the models each model family lists in the page settings.'
                    )
                  : t(
                      'The administrator has not chosen the models to compare yet.'
                    )}
              </EmptyDescription>
            </EmptyHeader>
            {isAdmin && (
              <Button type='button' onClick={() => setSettingsOpen(true)}>
                {t('Page settings')}
              </Button>
            )}
          </Empty>
        )}

        <Footnote formatting={formatting} />
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Price Comparison')}</SectionPageLayout.Title>
      {isAdmin && (
        <SectionPageLayout.Actions>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setSettingsOpen(true)}
          >
            <SlidersHorizontal data-icon='inline-start' />
            {t('Page settings')}
          </Button>
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>{content}</div>
        {isAdmin && (
          <PriceSettingsDialog
            open={settingsOpen}
            onOpenChange={setSettingsOpen}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
