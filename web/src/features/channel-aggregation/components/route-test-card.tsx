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
import { useMutation } from '@tanstack/react-query'
import { FlaskConical } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  escalationCeilingOptions,
  formatTierRatio,
  normalizeCeiling,
  preferenceLabel,
  SERVICE_TIER_PREFERENCES,
  SERVICE_TIERS,
  tierLabel,
  toggleTier,
} from '@/features/service-tiers/lib/tiers'
import type {
  ServiceTierKey,
  ServiceTierPreference,
  ServiceTierSettings,
} from '@/features/service-tiers/types'
import { toIntlLocale } from '@/i18n/languages'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { simulateRoute } from '../api'
import type { PolicyInput, Simulation } from '../types'

type RouteTestCardProps = {
  members: string[]
  groups: string[]
  draft: PolicyInput | null
}

function simulationReason(
  t: ReturnType<typeof useTranslation>['t'],
  reason?: string
): string {
  switch (reason) {
    case 'not_member':
      return t(
        'The model is not in any family, so it keeps ordinary group routing.'
      )
    case 'policy_disabled':
      return t(
        'Aggregation is off for this model, so it keeps ordinary group routing.'
      )
    case 'group_not_applicable':
      return t(
        'The rule does not apply to this user group, so it keeps ordinary group routing.'
      )
    case 'no_tier':
      return t(
        'No tier matches these user settings; the request would be rejected.'
      )
    default:
      return ''
  }
}

function channelReason(
  t: ReturnType<typeof useTranslation>['t'],
  reason?: string
): string {
  switch (reason) {
    case 'channel_missing':
      return t('Channel no longer exists')
    case 'disabled_in_pool':
      return t('Turned off in this pool')
    case 'channel_disabled':
      return t('Channel is disabled')
    case 'model_unsupported':
      return t('Does not support this model')
    case 'cooling_down':
      return t('Cooling down; tried last')
    default:
      return ''
  }
}

function scopeLabel(
  t: ReturnType<typeof useTranslation>['t'],
  scope: string
): string {
  if (scope === 'model') return t('Model override')
  if (scope === 'draft') return t('Unsaved changes')
  return t('Family default')
}

export function RouteTestCard(props: RouteTestCardProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [modelName, setModelName] = useState('')
  const [group, setGroup] = useState(
    props.groups.includes('default') ? 'default' : (props.groups[0] ?? '')
  )
  const [useDraft, setUseDraft] = useState(true)
  const [settings, setSettings] = useState<ServiceTierSettings>({
    enabled_tiers: [...SERVICE_TIERS],
    preference: 'lowest',
    allow_escalation: false,
    max_escalation_tier: '',
    version: 0,
  })
  const model = modelName || props.members[0] || ''
  const run = useMutation({
    mutationFn: (): Promise<Simulation> =>
      simulateRoute({
        model_name: model,
        user_group: group,
        settings,
        draft: useDraft && props.draft ? props.draft : undefined,
      }),
  })
  const ceilingOptions = escalationCeilingOptions(settings)
  const percent = new Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 0,
  })
  const result = run.data
  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-sm'>
          <FlaskConical className='size-4' aria-hidden='true' />
          {t('Route test')}
        </CardTitle>
        <CardDescription>
          {t(
            'Shows the tiers, channel order, ratios and price ceiling a request would get. Nothing is sent upstream and nothing is charged.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
          <div className='space-y-1.5'>
            <Label htmlFor='route-test-model'>{t('Model')}</Label>
            <Combobox
              id='route-test-model'
              options={props.members.map((name) => ({
                value: name,
                label: name,
              }))}
              value={model || null}
              placeholder={t('Choose a model')}
              onValueChange={(value) => setModelName(value ?? '')}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='route-test-group'>{t('User group')}</Label>
            <Select
              items={props.groups.map((value) => ({ value, label: value }))}
              value={group || null}
              onValueChange={(value) => setGroup(value ?? '')}
            >
              <SelectTrigger id='route-test-group' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {props.groups.map((value) => (
                    <SelectItem key={value} value={value}>
                      {value}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='route-test-preference'>
              {t('Default preference')}
            </Label>
            <Select
              items={SERVICE_TIER_PREFERENCES.map((value) => ({
                value,
                label: preferenceLabel(t, value),
              }))}
              value={settings.preference}
              onValueChange={(value) =>
                value &&
                setSettings(
                  normalizeCeiling({
                    ...settings,
                    preference: value as ServiceTierPreference,
                  })
                )
              }
            >
              <SelectTrigger id='route-test-preference' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {SERVICE_TIER_PREFERENCES.map((value) => (
                    <SelectItem key={value} value={value}>
                      {preferenceLabel(t, value)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='route-test-ceiling'>
              {t('Highest escalation tier')}
            </Label>
            <Select
              items={[
                { value: '', label: t('No escalation') },
                ...ceilingOptions.map((value) => ({
                  value,
                  label: tierLabel(t, value),
                })),
              ]}
              value={settings.max_escalation_tier}
              onValueChange={(value) =>
                setSettings(
                  value
                    ? {
                        ...settings,
                        allow_escalation: true,
                        max_escalation_tier: value as ServiceTierKey,
                      }
                    : {
                        ...settings,
                        allow_escalation: false,
                        max_escalation_tier: '',
                      }
                )
              }
            >
              <SelectTrigger id='route-test-ceiling' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value=''>{t('No escalation')}</SelectItem>
                  {ceilingOptions.map((value) => (
                    <SelectItem key={value} value={value}>
                      {tierLabel(t, value)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
          <span className='text-sm'>{t('User turned on')}</span>
          {SERVICE_TIERS.map((tier) => (
            <div key={tier} className='flex items-center gap-2'>
              <Checkbox
                id={`route-test-tier-${tier}`}
                checked={settings.enabled_tiers.includes(tier)}
                onCheckedChange={(checked) =>
                  setSettings(toggleTier(settings, tier, Boolean(checked)))
                }
              />
              <Label
                htmlFor={`route-test-tier-${tier}`}
                className='font-normal'
              >
                {tierLabel(t, tier)}
              </Label>
            </div>
          ))}
          <div className='flex items-center gap-2 sm:ml-auto'>
            <Switch
              id='route-test-draft'
              checked={useDraft}
              onCheckedChange={setUseDraft}
              disabled={!props.draft}
            />
            <Label htmlFor='route-test-draft' className='font-normal'>
              {t('Test unsaved changes')}
            </Label>
          </div>
          <Button
            type='button'
            onClick={() => run.mutate()}
            disabled={!model || run.isPending}
          >
            {run.isPending && <Spinner data-icon='inline-start' />}
            {t('Run test')}
          </Button>
        </div>
        {run.error && (
          <p role='alert' className='text-destructive text-sm'>
            {getServerErrorMessage(run.error)}
          </p>
        )}
        {result && (
          <div className='space-y-3 rounded-lg border p-3' aria-live='polite'>
            <div className='flex flex-wrap items-center gap-2 text-sm'>
              <span className='font-medium'>{result.model_name}</span>
              {result.family_name && (
                <Badge variant='secondary'>{result.family_name}</Badge>
              )}
              {result.policy_scope && (
                <Badge variant='outline'>
                  {scopeLabel(t, result.policy_scope)}
                </Badge>
              )}
              {result.applies && (
                <>
                  <Badge variant='outline' className='font-mono'>
                    {t('Price ceiling {{ratio}}', {
                      ratio: formatTierRatio(result.ceiling_ratio, locale),
                    })}
                  </Badge>
                  <Badge variant='outline'>
                    {t('Up to {{count}} attempts', {
                      count: result.max_attempts ?? 0,
                    })}
                  </Badge>
                </>
              )}
            </div>
            {!result.applies && (
              <p className='text-muted-foreground text-sm'>
                {simulationReason(t, result.reason)}
              </p>
            )}
            <ol className='space-y-3'>
              {result.steps.map((step, index) => (
                <li key={step.tier} className='space-y-1.5'>
                  <div className='flex flex-wrap items-center gap-2 text-sm'>
                    <span className='text-muted-foreground font-mono text-xs'>
                      {index + 1}.
                    </span>
                    <span className='font-medium'>
                      {tierLabel(t, step.tier)}
                    </span>
                    <span className='text-muted-foreground font-mono text-xs'>
                      {formatTierRatio(step.ratio, locale)}
                    </span>
                    <Badge variant={step.escalation ? 'outline' : 'secondary'}>
                      {step.escalation ? t('Escalation') : t('Preferred')}
                    </Badge>
                  </div>
                  <ul className='grid gap-1 pl-5'>
                    {step.channels.length === 0 && (
                      <li className='text-muted-foreground text-xs'>
                        {t('No channel in this tier yet')}
                      </li>
                    )}
                    {step.channels.map((channel) => (
                      <li
                        key={channel.channel_id}
                        className={cn(
                          'flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs',
                          !channel.eligible &&
                            'text-muted-foreground line-through decoration-1'
                        )}
                      >
                        <span className='font-medium'>
                          {channel.name || `#${channel.channel_id}`}
                        </span>
                        <span className='font-mono'>#{channel.channel_id}</span>
                        {channel.eligible && channel.level && (
                          <span>
                            {t('Level {{level}} · {{share}}', {
                              level: channel.level,
                              share: percent.format(channel.probability ?? 0),
                            })}
                          </span>
                        )}
                        {channel.reason && (
                          <span className='no-underline'>
                            ({channelReason(t, channel.reason)})
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ol>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
