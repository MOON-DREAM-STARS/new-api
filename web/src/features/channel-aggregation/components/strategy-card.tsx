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

import { MultiSelect } from '@/components/multi-select'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { ERROR_CATEGORIES, errorCategoryLabel } from '../lib/policy'
import type { AggregationStrategy } from '../types'

type StrategyCardProps = {
  strategy: AggregationStrategy
  groups: string[]
  disabled?: boolean
  onChange: (strategy: AggregationStrategy) => void
}

function NumberField(props: {
  id: string
  label: string
  description: string
  value: number
  min: number
  max: number
  disabled?: boolean
  onChange: (value: number) => void
}) {
  const invalid = !(props.value >= props.min && props.value <= props.max)
  return (
    <div className='space-y-1.5'>
      <Label htmlFor={props.id}>{props.label}</Label>
      <Input
        id={props.id}
        type='number'
        min={props.min}
        max={props.max}
        value={Number.isFinite(props.value) ? props.value : ''}
        disabled={props.disabled}
        aria-invalid={invalid || undefined}
        aria-describedby={`${props.id}-description`}
        onChange={(event) =>
          props.onChange(Math.trunc(event.target.valueAsNumber))
        }
        className='font-mono sm:w-36'
      />
      <p
        id={`${props.id}-description`}
        className='text-muted-foreground text-xs'
      >
        {props.description}
      </p>
    </div>
  )
}

function SwitchField(props: {
  id: string
  label: string
  description: string
  checked: boolean
  disabled?: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className='flex items-start justify-between gap-4 rounded-lg border p-3'>
      <div className='min-w-0 space-y-1'>
        <Label htmlFor={props.id}>{props.label}</Label>
        <p
          id={`${props.id}-description`}
          className='text-muted-foreground text-xs'
        >
          {props.description}
        </p>
      </div>
      <Switch
        id={props.id}
        checked={props.checked}
        disabled={props.disabled}
        aria-describedby={`${props.id}-description`}
        onCheckedChange={props.onChange}
      />
    </div>
  )
}

export function StrategyCard(props: StrategyCardProps) {
  const { t } = useTranslation()
  const strategy = props.strategy
  const set = (patch: Partial<AggregationStrategy>) =>
    props.onChange({ ...strategy, ...patch })
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-sm'>{t('Routing strategy')}</CardTitle>
        <CardDescription>
          {t(
            'How one request fails over. Escalation to a higher tier also requires the user to allow it.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-5'>
        <div className='grid gap-3 md:grid-cols-2'>
          <SwitchField
            id='strategy-same-tier'
            label={t('Fail over inside the tier')}
            description={t(
              'Try another channel of the same tier after a relayable error. This never changes the price.'
            )}
            checked={strategy.same_tier_failover}
            disabled={props.disabled}
            onChange={(checked) => set({ same_tier_failover: checked })}
          />
          <SwitchField
            id='strategy-skip-failed'
            label={t('Skip channels that already failed')}
            description={t(
              'A channel that failed in a request is not tried again in that request.'
            )}
            checked={strategy.skip_failed_channels}
            disabled={props.disabled}
            onChange={(checked) => set({ skip_failed_channels: checked })}
          />
        </div>
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          <NumberField
            id='strategy-max-attempts'
            label={t('Max attempts per request')}
            description={t('Across all tiers, 1 to 10')}
            value={strategy.max_attempts}
            min={1}
            max={10}
            disabled={props.disabled}
            onChange={(value) => set({ max_attempts: value })}
          />
          <NumberField
            id='strategy-max-seconds'
            label={t('Max total duration (s)')}
            description={t('No new attempt after this; 0 means no limit')}
            value={strategy.max_total_seconds}
            min={0}
            max={3600}
            disabled={props.disabled}
            onChange={(value) => set({ max_total_seconds: value })}
          />
          <NumberField
            id='strategy-cooldown-failures'
            label={t('Cooldown after failures')}
            description={t(
              'Consecutive failures before a channel cools down; 0 turns it off'
            )}
            value={strategy.cooldown_failures}
            min={0}
            max={20}
            disabled={props.disabled}
            onChange={(value) => set({ cooldown_failures: value })}
          />
          <NumberField
            id='strategy-cooldown-seconds'
            label={t('Cooldown duration (s)')}
            description={t(
              'A cooling channel is tried only when no other channel is left'
            )}
            value={strategy.cooldown_seconds}
            min={strategy.cooldown_failures > 0 ? 1 : 0}
            max={3600}
            disabled={props.disabled || strategy.cooldown_failures === 0}
            onChange={(value) => set({ cooldown_seconds: value })}
          />
        </div>
        <fieldset className='space-y-2'>
          <legend className='text-sm font-medium'>
            {t('Errors that may fail over')}
          </legend>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Request errors, local rejections and responses that already started never fail over. Gateway timeouts (504/524) follow the global no-retry rule.'
            )}
          </p>
          <div className='grid gap-2 sm:grid-cols-2'>
            {ERROR_CATEGORIES.map((category) => {
              const id = `strategy-error-${category}`
              return (
                <div key={category} className='flex items-center gap-2'>
                  <Checkbox
                    id={id}
                    checked={strategy.retry_on.includes(category)}
                    disabled={props.disabled}
                    onCheckedChange={(checked) =>
                      set({
                        retry_on: checked
                          ? [...strategy.retry_on, category]
                          : strategy.retry_on.filter(
                              (item) => item !== category
                            ),
                      })
                    }
                  />
                  <Label htmlFor={id} className='font-normal'>
                    {errorCategoryLabel(t, category)}
                  </Label>
                </div>
              )
            })}
          </div>
        </fieldset>
        <div className='space-y-1.5'>
          <Label htmlFor='strategy-user-groups'>
            {t('Applies to user groups')}
          </Label>
          <MultiSelect
            id='strategy-user-groups'
            options={props.groups.map((group) => ({
              value: group,
              label: group,
            }))}
            selected={strategy.user_groups}
            onChange={(values) => set({ user_groups: values })}
            placeholder={t('All user groups')}
            disabled={props.disabled}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Users outside these groups keep ordinary group routing for this family. Leave empty for everyone.'
            )}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
