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
import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Spinner } from '@/components/ui/spinner'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { formatUsd } from './lib'
import type { ModelsDevCatalogItem } from './types'

interface PricesProps {
  input: number
  output: number
  cacheRead?: number | undefined
  cacheWrite?: number | undefined
  tiers?: number | undefined
  className?: string
}

/** The list prices of a models.dev entry, in dollars per million tokens. */
export function EntryPrices(props: PricesProps) {
  const { t, i18n } = useTranslation()
  const prices: [string, number | undefined][] = [
    [t('Input'), props.input],
    [t('Output'), props.output],
    [t('Cache read'), props.cacheRead],
    [t('Cache write'), props.cacheWrite],
  ]
  return (
    <p
      className={cn(
        'text-muted-foreground flex flex-wrap gap-x-3 gap-y-0.5 text-xs',
        props.className
      )}
    >
      {prices
        .filter(([, value]) => value !== undefined)
        .map(([label, value]) => (
          <span key={label} className='whitespace-nowrap'>
            {label}{' '}
            <span className='text-foreground tabular-nums'>
              {formatUsd(value, i18n.language)}
            </span>
          </span>
        ))}
      <span className='whitespace-nowrap'>{t('per 1M tokens')}</span>
      {props.tiers ? (
        <span className='whitespace-nowrap'>
          {t('{{count}} context tiers', { count: props.tiers })}
        </span>
      ) : null}
    </p>
  )
}

interface EntryRowProps {
  item: ModelsDevCatalogItem
  leading?: ReactNode
  trailing?: ReactNode
}

/** One models.dev entry in a list the administrator picks from. */
export function EntryRow({ item, leading, trailing }: EntryRowProps) {
  const { t } = useTranslation()
  return (
    <li className='flex items-start gap-3 rounded-lg border p-3'>
      {leading}
      <div className='min-w-0 flex-1 space-y-1.5'>
        <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
          <span className='min-w-0 font-medium break-words'>
            {item.model_name || item.model_id}
          </span>
          <span className='text-muted-foreground min-w-0 font-mono text-xs break-all'>
            {item.key}
          </span>
        </div>
        <div className='flex flex-wrap items-center gap-1.5'>
          <span className='text-muted-foreground text-xs'>
            {item.provider_name}
          </span>
          {item.official ? (
            <Badge variant='secondary'>{t('Official')}</Badge>
          ) : null}
          {item.common ? <Badge variant='outline'>{t('Popular')}</Badge> : null}
          {item.site_models?.map((name) => (
            <Badge key={name} variant='outline'>
              {t('Your model {{name}}', { name })}
            </Badge>
          ))}
          {item.mapped_from?.map((name) => (
            <Badge key={name} variant='outline'>
              {t('Mapped from {{name}}', { name })}
            </Badge>
          ))}
        </div>
        <EntryPrices
          input={item.input}
          output={item.output}
          cacheRead={item.cache_read}
          cacheWrite={item.cache_write}
          tiers={item.tiers?.length}
        />
      </div>
      {trailing}
    </li>
  )
}

interface ListStateProps {
  query: UseQueryResult<unknown>
  empty: boolean
  children: ReactNode
}

/** What a list of models.dev entries shows while it loads, fails or is empty. */
export function ListState({ query, empty, children }: ListStateProps) {
  const { t } = useTranslation()
  if (query.isError && !query.data) {
    return (
      <ErrorState
        description={getServerErrorMessage(query.error)}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isPending) {
    return (
      <div className='flex justify-center py-10'>
        <Spinner />
      </div>
    )
  }
  if (empty) {
    return (
      <p className='text-muted-foreground py-10 text-center text-sm'>
        {t('No models found')}
      </p>
    )
  }
  return children
}
