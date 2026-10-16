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
import { useMutation, useQuery } from '@tanstack/react-query'
import { ChevronDown, RefreshCw } from 'lucide-react'
import { useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Spinner } from '@/components/ui/spinner'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { modelsDevKeys, previewModelsDev, syncModelsDev } from './api'
import { EntryPickerDialog } from './entry-picker-dialog'
import {
  allowImport,
  buildSyncRequest,
  firstFilter,
  isWritable,
  keepOut,
  rowFilterOf,
  withMapping,
  type RowFilter,
} from './lib'
import { PlanRow } from './plan-row'
import type {
  ModelsDevConfig,
  ModelsDevPlan,
  ModelsDevSyncResult,
} from './types'

const PAGE_SIZE = 60
const UNMATCHED_PAGE_SIZE = 40

interface Selection {
  hash: string
  known: ReadonlySet<string>
  checked: ReadonlySet<string>
}

/**
 * The rows ticked for the import. A new preview keeps the ticks of the rows
 * the administrator already saw; rows that need a decision start unticked.
 */
function nextSelection(
  plan: ModelsDevPlan,
  previous: Selection | null
): Selection {
  const checked = new Set<string>()
  for (const row of plan.rows) {
    if (!isWritable(row)) continue
    const seen = previous?.known.has(row.model) ?? false
    if (seen ? previous?.checked.has(row.model) : !row.review) {
      checked.add(row.model)
    }
  }
  return {
    hash: plan.hash,
    known: new Set(plan.rows.map((row) => row.model)),
    checked,
  }
}

interface UnmatchedListProps {
  names: string[]
  onMap: (name: string) => void
}

/** Models of this site that models.dev does not know by name. */
function UnmatchedList({ names, onMap }: UnmatchedListProps) {
  const { t } = useTranslation()
  const [limit, setLimit] = useState(UNMATCHED_PAGE_SIZE)
  return (
    <Collapsible className='rounded-lg border'>
      <CollapsibleTrigger
        render={
          <Button
            variant='ghost'
            className='w-full justify-between rounded-lg px-3'
          />
        }
      >
        {t('{{count}} models of your site have no models.dev price', {
          count: names.length,
        })}
        <ChevronDown aria-hidden='true' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <p className='text-muted-foreground px-3 pb-2 text-xs'>
          {t(
            'models.dev does not list these names. If it lists the same model under another name, map it.'
          )}
        </p>
        <ul className='divide-y border-t'>
          {names.slice(0, limit).map((name) => (
            <li
              key={name}
              className='flex items-center justify-between gap-3 px-3 py-1.5'
            >
              <span className='min-w-0 text-sm break-all'>{name}</span>
              <Button
                size='sm'
                variant='outline'
                onClick={() => onMap(name)}
                aria-label={t('Map {{model}}', { model: name })}
              >
                {t('Map')}
              </Button>
            </li>
          ))}
        </ul>
        {limit < names.length ? (
          <Button
            variant='ghost'
            className='w-full'
            onClick={() => setLimit(limit + UNMATCHED_PAGE_SIZE)}
          >
            {t('Show more')}
          </Button>
        ) : null}
      </CollapsibleContent>
    </Collapsible>
  )
}

interface SyncDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: ModelsDevConfig
  /** Stores a changed configuration; resolves to false when that failed. */
  onSaveConfig: (config: ModelsDevConfig) => Promise<boolean>
  onSynced: (result: ModelsDevSyncResult) => void
}

/** Shows what an import would do and writes the rows the administrator ticked. */
export function SyncDialog(props: SyncDialogProps) {
  const { t } = useTranslation()
  const { config } = props
  const forceRefresh = useRef(false)
  const [filter, setFilter] = useState<RowFilter | null>(null)
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [selection, setSelection] = useState<Selection | null>(null)
  const [mapTarget, setMapTarget] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [wasOpen, setWasOpen] = useState(props.open)
  if (props.open !== wasOpen) {
    setWasOpen(props.open)
    if (props.open) {
      setFilter(null)
      setLimit(PAGE_SIZE)
      setSelection(null)
      setMapTarget(null)
    }
  }

  const preview = useQuery({
    queryKey: modelsDevKeys.preview,
    queryFn: () => {
      const refresh = forceRefresh.current
      forceRefresh.current = false
      return previewModelsDev(refresh)
    },
    enabled: props.open,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const plan = preview.data
  if (plan && plan.hash !== selection?.hash) {
    setSelection(nextSelection(plan, selection))
  }

  const counts = useMemo(() => {
    const total: Record<RowFilter, number> = {
      changes: 0,
      locked: 0,
      unchanged: 0,
      invalid: 0,
    }
    for (const row of plan?.rows ?? []) total[rowFilterOf(row)] += 1
    return total
  }, [plan])
  const active = filter ?? firstFilter(counts)
  const rows = (plan?.rows ?? []).filter((row) => rowFilterOf(row) === active)
  const checked = selection?.checked ?? new Set<string>()

  const sync = useMutation({
    mutationFn: syncModelsDev,
    onSuccess: (result) => {
      props.onSynced(result)
      props.onOpenChange(false)
    },
    onError: (error) => {
      handleServerError(error, t('Failed to import prices'))
      void preview.refetch()
    },
  })

  const edit = async (next: ModelsDevConfig) => {
    setBusy(true)
    try {
      return await props.onSaveConfig(next)
    } finally {
      setBusy(false)
    }
  }
  const tick = (model: string, on: boolean) =>
    setSelection((current) => {
      if (!current) return current
      const next = new Set(current.checked)
      if (on) next.add(model)
      else next.delete(model)
      return { ...current, checked: next }
    })
  const refresh = () => {
    forceRefresh.current = true
    void preview.refetch()
  }
  const filters: [RowFilter, string][] = [
    ['changes', t('Changes')],
    ['locked', t('Locked')],
    ['unchanged', t('Up to date')],
    ['invalid', t('Problems')],
  ]

  const waiting = (plan?.rows ?? []).filter(
    (row) => isWritable(row) && row.review && !checked.has(row.model)
  ).length
  let body: ReactNode
  if (preview.isError) {
    body = (
      <ErrorState
        description={getServerErrorMessage(preview.error)}
        onRetry={() => void preview.refetch()}
      />
    )
  } else if (!plan) {
    body = (
      <div className='flex justify-center py-10'>
        <Spinner />
      </div>
    )
  } else {
    body = (
      <>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <p className='text-muted-foreground text-xs'>
            {t('models.dev data from {{time}}', {
              time: formatTimestampToDate(plan.fetched_at),
            })}
          </p>
          <Button
            size='sm'
            variant='outline'
            disabled={preview.isFetching}
            onClick={refresh}
          >
            <RefreshCw
              aria-hidden='true'
              className={preview.isFetching ? 'animate-spin' : undefined}
            />
            {t('Refresh models.dev data')}
          </Button>
        </div>
        <ToggleGroup
          value={[active]}
          onValueChange={(values) => {
            const next = filters.find(([key]) => key === values[0])?.[0]
            if (next) {
              setFilter(next)
              setLimit(PAGE_SIZE)
            }
          }}
          variant='outline'
          size='sm'
          className='flex-wrap'
          aria-label={t('Show')}
        >
          {filters.map(([key, label]) => (
            <ToggleGroupItem key={key} value={key}>
              {label} ({counts[key]})
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        {waiting > 0 ? (
          <p className='text-warning text-xs' role='status'>
            {t(
              '{{count}} proposals wait for your decision. Tick them to import.',
              {
                count: waiting,
              }
            )}
          </p>
        ) : null}
        {rows.length === 0 ? (
          <p className='text-muted-foreground py-8 text-center text-sm'>
            {t('Nothing to show here')}
          </p>
        ) : (
          <ul className='space-y-2'>
            {rows.slice(0, limit).map((row) => (
              <PlanRow
                key={row.model}
                row={row}
                checked={checked.has(row.model)}
                busy={busy}
                onCheckedChange={(on) => tick(row.model, on)}
                onAllowImport={() => void edit(allowImport(config, row.model))}
                onKeepOut={() => void edit(keepOut(config, row.model))}
                onMap={() => setMapTarget(row.model)}
                onUnmap={() => void edit(withMapping(config, row.model, null))}
              />
            ))}
          </ul>
        )}
        {rows.length > limit ? (
          <Button
            variant='ghost'
            className='self-center'
            onClick={() => setLimit(limit + PAGE_SIZE)}
          >
            {t('Show more')}
          </Button>
        ) : null}
        {plan.unmatched.length > 0 ? (
          <UnmatchedList names={plan.unmatched} onMap={setMapTarget} />
        ) : null}
      </>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import prices from models.dev')}
      description={t(
        'Nothing is written until you click Import. A model that already has a price stays locked unless you allow the import.'
      )}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(60vh, 36rem)'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={!plan || checked.size === 0 || sync.isPending || busy}
            onClick={() => plan && sync.mutate(buildSyncRequest(plan, checked))}
          >
            {sync.isPending ? <Spinner /> : null}
            {t('Import {{count}} models', { count: checked.size })}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-3'>{body}</div>
      <EntryPickerDialog
        open={mapTarget !== null}
        onOpenChange={(open) => {
          if (!open) setMapTarget(null)
        }}
        title={t('Map {{model}} to a models.dev model', {
          model: mapTarget ?? '',
        })}
        description={t(
          'The price of the model you pick is imported for this model.'
        )}
        initialQuery={mapTarget ?? ''}
        pickLabel={t('Use this price')}
        picking={busy}
        onPick={async (item) => {
          if (
            mapTarget &&
            (await edit(withMapping(config, mapTarget, item.key)))
          ) {
            setMapTarget(null)
          }
        }}
      />
    </Dialog>
  )
}
