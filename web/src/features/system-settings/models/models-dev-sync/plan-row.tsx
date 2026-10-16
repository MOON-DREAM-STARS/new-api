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
import { AlertTriangle, Link2, Link2Off, Lock, LockOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DataTableRowActionMenu } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'

import { EntryPrices } from './catalog-entry'
import { currentPriceOf, formatUsd, isWritable } from './lib'
import type { ModelsDevRow } from './types'

type Translate = (key: string, options?: Record<string, unknown>) => string

function statusLabel(row: ModelsDevRow, t: Translate): string {
  switch (row.status) {
    case 'new':
      return t('New price')
    case 'update':
      return t('Price update')
    case 'unchanged':
      return t('Up to date')
    case 'locked':
      return lockLabel(row.lock_reason, t)
    default:
      return t('Cannot import')
  }
}

function lockLabel(reason: ModelsDevRow['lock_reason'], t: Translate): string {
  switch (reason) {
    case 'explicit':
      return t('Locked by you')
    case 'modified':
      return t('Edited after the last import')
    case 'configured':
      return t('Already has a price')
    default:
      return t('Built-in price')
  }
}

function sourceLabel(source: ModelsDevRow['source'], t: Translate): string {
  switch (source) {
    case 'mapping':
      return t('Mapped by you')
    case 'selected':
      return t('Picked by hand')
    case 'site':
      return t('Same name on models.dev')
    default:
      return t('Popular model')
  }
}

/** Why the proposal waits for a decision, or null when it needs none. */
function reviewLabel(row: ModelsDevRow, t: Translate): string | null {
  if (!row.review || !isWritable(row)) return null
  if (row.review === 'reseller') return t('Reseller price')
  const change = row.change ?? 1
  const factor = (change >= 1 ? change : 1 / change).toFixed(1)
  return change >= 1
    ? t('Price up {{factor}}×', { factor })
    : t('Price down {{factor}}×', { factor })
}

const statusVariant = {
  new: 'default',
  update: 'secondary',
  unchanged: 'outline',
  locked: 'outline',
  invalid: 'destructive',
} as const

interface PlanRowProps {
  row: ModelsDevRow
  checked: boolean
  busy: boolean
  onCheckedChange: (checked: boolean) => void
  onAllowImport: () => void
  onKeepOut: () => void
  onMap: () => void
  onUnmap: () => void
}

/** One proposal of the import: what the model costs now and what models.dev says. */
export function PlanRow(props: PlanRowProps) {
  const { t, i18n } = useTranslation()
  const { row } = props
  const writable = isWritable(row)
  const now = currentPriceOf(row)
  const review = reviewLabel(row, t)
  return (
    <li
      className='flex items-start gap-3 rounded-lg border p-3'
      data-status={row.status}
    >
      {writable ? (
        <Checkbox
          className='mt-1'
          checked={props.checked}
          aria-label={row.model}
          onCheckedChange={props.onCheckedChange}
        />
      ) : (
        <span className='mt-1 size-4 shrink-0' aria-hidden='true' />
      )}
      <div className='min-w-0 flex-1 space-y-1.5'>
        <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
          <span className='min-w-0 font-medium break-words'>{row.model}</span>
          <Badge variant={statusVariant[row.status]}>
            {row.status === 'locked' ? <Lock aria-hidden='true' /> : null}
            {statusLabel(row, t)}
          </Badge>
          {review ? (
            <Badge variant='warning'>
              <AlertTriangle aria-hidden='true' />
              {review}
            </Badge>
          ) : null}
        </div>
        {row.key ? (
          <p className='text-muted-foreground flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs'>
            <span>{sourceLabel(row.source, t)}</span>
            <span aria-hidden='true'>·</span>
            <span className='font-mono break-all'>{row.key}</span>
            {row.official ? (
              <Badge variant='secondary'>{t('Official')}</Badge>
            ) : null}
            {!row.official && row.status !== 'invalid' ? (
              <Badge variant='outline'>{t('Reseller')}</Badge>
            ) : null}
          </p>
        ) : null}
        {row.status === 'invalid' ? (
          <p className='text-destructive text-xs'>
            {row.error === 'models.dev no longer lists this model'
              ? t('models.dev no longer lists this model')
              : row.error}
          </p>
        ) : (
          <EntryPrices
            input={row.input}
            output={row.output}
            cacheRead={row.cache_read}
            cacheWrite={row.cache_write}
            tiers={row.tiers}
          />
        )}
        <p className='text-muted-foreground text-xs'>
          {now
            ? t('Now: input {{input}} · output {{output}}', {
                input: formatUsd(now.input, i18n.language),
                output: formatUsd(now.output, i18n.language),
              })
            : t('No price set yet')}
          {now?.timeBased ? ` ${t('(peak price)')}` : ''}
        </p>
        {row.review === 'reseller' && writable ? (
          <p className='text-warning text-xs'>
            {t(
              'models.dev lists no price from the vendor itself, so this is the price of a reseller. Tick the model to import it anyway.'
            )}
          </p>
        ) : null}
        {row.review === 'large_change' && writable ? (
          <p className='text-warning text-xs'>
            {t(
              'The price moved far from the one the last import wrote. Tick the model to import it anyway.'
            )}
          </p>
        ) : null}
      </div>
      <DataTableRowActionMenu
        ariaLabel={t('Actions for {{model}}', { model: row.model })}
      >
        {row.status === 'locked' ? (
          <DropdownMenuItem disabled={props.busy} onClick={props.onAllowImport}>
            <LockOpen aria-hidden='true' />
            {t('Allow import')}
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem disabled={props.busy} onClick={props.onKeepOut}>
            <Lock aria-hidden='true' />
            {t('Keep out of imports')}
          </DropdownMenuItem>
        )}
        {row.source === 'site' || row.source === 'mapping' ? (
          <DropdownMenuItem disabled={props.busy} onClick={props.onMap}>
            <Link2 aria-hidden='true' />
            {t('Use another models.dev model')}
          </DropdownMenuItem>
        ) : null}
        {row.source === 'mapping' ? (
          <DropdownMenuItem disabled={props.busy} onClick={props.onUnmap}>
            <Link2Off aria-hidden='true' />
            {t('Remove mapping')}
          </DropdownMenuItem>
        ) : null}
      </DataTableRowActionMenu>
    </li>
  )
}
