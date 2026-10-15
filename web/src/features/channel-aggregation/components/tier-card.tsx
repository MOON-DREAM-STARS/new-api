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
import { Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { TierIcon } from '@/features/service-tiers/components/tier-icon'
import { tierLabel } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierKey } from '@/features/service-tiers/types'
import { cn } from '@/lib/utils'

import { CHANNEL_STATUS_ENABLED } from '../lib/policy'
import type { AggregationChannel, AggregationTierConfig } from '../types'

type TierCardProps = {
  tier: ServiceTierKey
  config: AggregationTierConfig
  channels: Map<number, AggregationChannel>
  disabled?: boolean
  onChange: (config: AggregationTierConfig) => void
  onEditPool: () => void
}

export function TierCard(props: TierCardProps) {
  const { t } = useTranslation()
  const entries = props.config.channels
  const active = entries.filter((entry) => entry.enabled)
  const now = Date.now() / 1000
  let healthy = 0
  let unavailable = 0
  let cooling = 0
  for (const entry of active) {
    const channel = props.channels.get(entry.channel_id)
    if (!channel || channel.status !== CHANNEL_STATUS_ENABLED) unavailable++
    else if ((channel.health.cooldown_until ?? 0) > now) cooling++
    else healthy++
  }
  const ratioId = `tier-${props.tier}-ratio`
  const labelId = `tier-${props.tier}-label`
  return (
    <div
      data-tier={props.tier}
      className={cn(
        'bg-card flex min-w-0 flex-col gap-4 rounded-xl border p-4',
        props.config.enabled ? 'border-primary/40' : 'border-dashed'
      )}
    >
      <div className='flex items-center justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span
            className={cn(
              'flex size-8 shrink-0 items-center justify-center rounded-lg border',
              props.config.enabled
                ? 'bg-primary/10 text-primary border-primary/20'
                : 'bg-muted text-muted-foreground'
            )}
          >
            <TierIcon tier={props.tier} className='size-4' />
          </span>
          <span id={labelId} className='truncate text-sm font-semibold'>
            {tierLabel(t, props.tier)}
          </span>
        </div>
        <Switch
          checked={props.config.enabled}
          disabled={props.disabled}
          aria-labelledby={labelId}
          onCheckedChange={(enabled) =>
            props.onChange({ ...props.config, enabled })
          }
        />
      </div>
      <div className='grid grid-cols-2 gap-3'>
        <div className='space-y-1.5'>
          <Label htmlFor={ratioId} className='text-muted-foreground text-xs'>
            {t('Billing ratio')}
          </Label>
          <Input
            id={ratioId}
            type='number'
            inputMode='decimal'
            min={0}
            max={1000}
            step={0.01}
            value={
              Number.isFinite(props.config.ratio) ? props.config.ratio : ''
            }
            disabled={props.disabled}
            aria-invalid={
              !(props.config.ratio > 0 && props.config.ratio <= 1000) ||
              undefined
            }
            onChange={(event) =>
              props.onChange({
                ...props.config,
                ratio: event.target.valueAsNumber,
              })
            }
            className='font-mono'
          />
        </div>
        <div className='space-y-1.5'>
          <span className='text-muted-foreground block text-xs'>
            {t('Channels')}
          </span>
          <span className='flex h-8 items-center text-sm font-medium'>
            {t('{{active}} active / {{total}}', {
              active: active.length,
              total: entries.length,
            })}
          </span>
        </div>
      </div>
      <div className='flex flex-wrap gap-1.5' aria-label={t('Channel health')}>
        <Badge variant='secondary'>
          {t('{{count}} healthy', { count: healthy })}
        </Badge>
        {cooling > 0 && (
          <Badge variant='warning'>
            {t('{{count}} cooling down', { count: cooling })}
          </Badge>
        )}
        {unavailable > 0 && (
          <Badge variant='destructive'>
            {t('{{count}} unavailable', { count: unavailable })}
          </Badge>
        )}
        {props.config.enabled && active.length === 0 && (
          <Badge variant='destructive'>{t('No enabled channel')}</Badge>
        )}
      </div>
      <Button
        type='button'
        variant='outline'
        className='mt-auto'
        onClick={props.onEditPool}
        disabled={props.disabled}
      >
        <Settings2 data-icon='inline-start' aria-hidden='true' />
        {t('Edit channel pool')}
      </Button>
    </div>
  )
}
