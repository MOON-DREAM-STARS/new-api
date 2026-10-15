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
import { Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { tierLabel } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierKey } from '@/features/service-tiers/types'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampToDate } from '@/lib/format'

import { getChannelOptions } from '../api'
import {
  CHANNEL_STATUS_ENABLED,
  channelStatusLabel,
  errorCategoryLabel,
  poolPositions,
  sortPool,
} from '../lib/policy'
import type { AggregationChannel, AggregationTierChannel } from '../types'

type ChannelPoolSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  tier: ServiceTierKey
  familyId: number
  modelName: string
  memberCount: number
  entries: AggregationTierChannel[]
  onApply: (entries: AggregationTierChannel[]) => void
}

export function ChannelPoolSheet(props: ChannelPoolSheetProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [entries, setEntries] = useState<AggregationTierChannel[]>([])
  useEffect(() => {
    if (props.open) setEntries(sortPool(props.entries))
  }, [props.open, props.entries])
  const options = useQuery({
    queryKey: [
      'channel-aggregation',
      'channels',
      props.familyId,
      props.modelName,
    ],
    queryFn: () => getChannelOptions(props.familyId, props.modelName),
    enabled: props.open,
  })
  const channels = useMemo(
    () => new Map((options.data ?? []).map((channel) => [channel.id, channel])),
    [options.data]
  )
  const positions = poolPositions(entries)
  const percent = new Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 0,
  })
  const compatible = (channel: AggregationChannel) =>
    props.modelName ? channel.supports_model : channel.supported_members > 0
  const addOptions = (options.data ?? []).map((channel) => {
    const added = entries.some((entry) => entry.channel_id === channel.id)
    let coverage = t('Serves {{count}}/{{total}} models', {
      count: channel.supported_members,
      total: props.memberCount,
    })
    if (props.modelName) {
      coverage = channel.supports_model
        ? t('Supports this model')
        : t('Does not support this model')
    }
    return {
      value: String(channel.id),
      label: `${channel.name} #${channel.id}`,
      description: `${channelStatusLabel(t, channel.status)} · ${coverage}`,
      disabled: added || !compatible(channel),
    }
  })
  const nextPriority =
    entries.length > 0
      ? Math.min(...entries.map((entry) => entry.priority)) - 10
      : 100
  const update = (channelId: number, patch: Partial<AggregationTierChannel>) =>
    setEntries((current) =>
      current.map((entry) =>
        entry.channel_id === channelId ? { ...entry, ...patch } : entry
      )
    )

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full gap-0 sm:max-w-2xl'>
        <SheetHeader className='border-b'>
          <SheetTitle>
            {t('{{tier}} channel pool', { tier: tierLabel(t, props.tier) })}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Higher priority is tried first; channels with the same priority share traffic by weight. Failed channels are skipped for the rest of the request.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='min-h-0 flex-1 space-y-4 overflow-y-auto p-4'>
          <div className='space-y-1.5'>
            <Label htmlFor='pool-add-channel'>{t('Add channel')}</Label>
            {options.isLoading ? (
              <Skeleton className='h-8 w-full' />
            ) : (
              <Combobox
                id='pool-add-channel'
                options={addOptions}
                placeholder={t('Search existing channels')}
                emptyText={t('No matching channels')}
                onValueChange={(value: string | null) => {
                  if (!value) return
                  setEntries((current) => [
                    ...current,
                    {
                      channel_id: Number(value),
                      priority: nextPriority,
                      weight: 1,
                      enabled: true,
                    },
                  ])
                }}
              />
            )}
            <p className='text-muted-foreground text-xs'>
              {t(
                'Channels are managed in Channels; a pool only references them. Incompatible channels cannot be added.'
              )}
            </p>
          </div>
          {entries.length === 0 ? (
            <p className='text-muted-foreground rounded-lg border border-dashed py-8 text-center text-sm'>
              {t('No channel in this tier yet')}
            </p>
          ) : (
            <ol className='space-y-2' aria-label={t('Channel pool')}>
              {entries.map((entry) => {
                const channel = channels.get(entry.channel_id)
                const position = positions.get(entry.channel_id)
                const health = channel?.health
                const unavailable =
                  !channel || channel.status !== CHANNEL_STATUS_ENABLED
                const id = `pool-${entry.channel_id}`
                return (
                  <li
                    key={entry.channel_id}
                    className='bg-card rounded-lg border p-3'
                  >
                    <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
                      <div className='min-w-0 space-y-1'>
                        <div className='flex flex-wrap items-center gap-1.5'>
                          <span className='truncate text-sm font-medium'>
                            {channel?.name ?? t('Unknown channel')}
                          </span>
                          <span className='text-muted-foreground font-mono text-xs'>
                            #{entry.channel_id}
                          </span>
                          <Badge
                            variant={unavailable ? 'destructive' : 'secondary'}
                          >
                            {channel
                              ? channelStatusLabel(t, channel.status)
                              : t('Missing')}
                          </Badge>
                          {position ? (
                            <Badge variant='outline'>
                              {t('Level {{level}} · {{share}}', {
                                level: position.level,
                                share: percent.format(position.share),
                              })}
                            </Badge>
                          ) : (
                            <Badge variant='outline'>{t('Not routed')}</Badge>
                          )}
                        </div>
                        <p className='text-muted-foreground text-xs break-words'>
                          {channel?.status_reason &&
                            `${channel.status_reason} · `}
                          {health && health.recent_failures > 0
                            ? t(
                                '{{count}} failures in 15 min, last {{status}} {{category}} at {{time}}',
                                {
                                  count: health.recent_failures,
                                  status: health.last_status ?? '',
                                  category: errorCategoryLabel(
                                    t,
                                    health.last_category ?? ''
                                  ),
                                  time: formatTimestampToDate(
                                    health.last_failure_at
                                  ),
                                }
                              )
                            : t('No recent failures on this node')}
                        </p>
                      </div>
                      <div className='flex shrink-0 flex-wrap items-end gap-2'>
                        <div className='space-y-1'>
                          <Label
                            htmlFor={`${id}-priority`}
                            className='text-muted-foreground text-xs'
                          >
                            {t('Priority')}
                          </Label>
                          <Input
                            id={`${id}-priority`}
                            type='number'
                            value={entry.priority}
                            className='w-24 font-mono'
                            onChange={(event) =>
                              update(entry.channel_id, {
                                priority: Math.trunc(
                                  event.target.valueAsNumber || 0
                                ),
                              })
                            }
                            onBlur={() =>
                              setEntries((current) => sortPool(current))
                            }
                          />
                        </div>
                        <div className='space-y-1'>
                          <Label
                            htmlFor={`${id}-weight`}
                            className='text-muted-foreground text-xs'
                          >
                            {t('Weight')}
                          </Label>
                          <Input
                            id={`${id}-weight`}
                            type='number'
                            min={0}
                            max={10000}
                            value={entry.weight}
                            className='w-20 font-mono'
                            onChange={(event) =>
                              update(entry.channel_id, {
                                weight: Math.min(
                                  10000,
                                  Math.max(
                                    0,
                                    Math.trunc(event.target.valueAsNumber || 0)
                                  )
                                ),
                              })
                            }
                            onBlur={() =>
                              setEntries((current) => sortPool(current))
                            }
                          />
                        </div>
                        <div className='flex h-8 items-center gap-2'>
                          <Switch
                            checked={entry.enabled}
                            aria-label={t('Use {{name}} in this tier', {
                              name: channel?.name ?? entry.channel_id,
                            })}
                            onCheckedChange={(enabled) =>
                              update(entry.channel_id, { enabled })
                            }
                          />
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            aria-label={t('Remove {{name}}', {
                              name: channel?.name ?? entry.channel_id,
                            })}
                            onClick={() =>
                              setEntries((current) =>
                                current.filter(
                                  (item) => item.channel_id !== entry.channel_id
                                )
                              )
                            }
                          >
                            <Trash2 className='size-4' aria-hidden='true' />
                          </Button>
                        </div>
                      </div>
                    </div>
                  </li>
                )
              })}
            </ol>
          )}
        </div>
        <SheetFooter className='border-t'>
          <div className='flex w-full flex-wrap justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={() => props.onOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={() => {
                props.onApply(sortPool(entries))
                props.onOpenChange(false)
              }}
            >
              {t('Apply to draft')}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
