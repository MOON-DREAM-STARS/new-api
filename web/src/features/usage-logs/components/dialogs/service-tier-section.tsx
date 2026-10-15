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
import { Layers3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { errorCategoryLabel } from '@/features/channel-aggregation/lib/policy'
import {
  formatTierRatio,
  preferenceLabel,
  tierLabel,
} from '@/features/service-tiers/lib/tiers'
import type { ServiceTierPreference } from '@/features/service-tiers/types'
import { toIntlLocale } from '@/i18n/languages'

import type { LogOtherData } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

type ServiceTierSectionProps = {
  other: LogOtherData
  isAdmin: boolean
}

/** The service tier that served a request; the attempt trail is admin-only. */
export function ServiceTierSection(props: ServiceTierSectionProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const info = props.other.service_tier
  if (!info) return null
  const route = props.isAdmin
    ? props.other.admin_info?.service_tier_route
    : undefined
  return (
    <DetailSection
      label={t('Service tier')}
      icon={<Layers3 className='size-4' />}
    >
      <div className='min-w-0 space-y-1'>
        <DetailRow label={t('Model family')} value={info.family} />
        <DetailRow
          label={t('Default preference')}
          value={preferenceLabel(t, info.preference as ServiceTierPreference)}
        />
        <DetailRow
          label={t('Preferred tier')}
          value={tierLabel(t, info.preferred_tier)}
        />
        <DetailRow
          label={t('Serving tier')}
          value={
            info.escalated
              ? t('{{tier}} (escalated)', {
                  tier: tierLabel(t, info.final_tier),
                })
              : tierLabel(t, info.final_tier)
          }
        />
        <DetailRow
          label={t('Billing ratio')}
          value={formatTierRatio(info.ratio, locale)}
          mono
        />
        <DetailRow label={t('Attempts')} value={info.attempts} mono />
        {route && (
          <DetailRow
            label={t('Price ceiling')}
            value={formatTierRatio(route.ceiling_ratio, locale)}
            mono
          />
        )}
      </div>
      {route && route.attempts.length > 0 && (
        <ol
          className='mt-2 space-y-1 border-l pl-3'
          aria-label={t('Routing decisions')}
        >
          {route.attempts.map((attempt) => (
            <li key={attempt.attempt} className='text-xs break-words'>
              <span className='font-mono'>#{attempt.attempt}</span>{' '}
              {tierLabel(t, attempt.tier)} ·{' '}
              {attempt.channel_name || attempt.channel_id} (#
              {attempt.channel_id}) ·{' '}
              {attempt.outcome === 'success' && t('Succeeded')}
              {attempt.outcome === 'failure' &&
                t('Failed {{status}} {{category}}', {
                  status: attempt.status ?? '',
                  category: errorCategoryLabel(t, attempt.category ?? ''),
                })}
              {attempt.outcome === 'blocked' &&
                t('Not called: insufficient quota for this tier')}
              {attempt.outcome === 'pending' && t('In progress')}
              {attempt.decision && attempt.outcome === 'failure' && (
                <span className='text-muted-foreground'>
                  {' '}
                  → {routeDecisionLabel(t, attempt.decision)}
                </span>
              )}
            </li>
          ))}
        </ol>
      )}
    </DetailSection>
  )
}

function routeDecisionLabel(
  t: ReturnType<typeof useTranslation>['t'],
  decision: string
): string {
  switch (decision) {
    case 'same_tier_failover':
      return t('next channel in the same tier')
    case 'tier_escalation':
      return t('escalated to a higher tier')
    case 'response_started':
      return t('stopped: the response had started')
    case 'client_cancelled':
      return t('stopped: the client disconnected')
    case 'non_retryable_error':
      return t('stopped: the error cannot fail over')
    case 'error_not_relayable':
      return t('stopped: the policy does not fail over this error')
    case 'system_retry_exclusion':
      return t('stopped: excluded by the global retry rule')
    case 'attempt_budget_exhausted':
      return t('stopped: attempt limit reached')
    case 'time_budget_exhausted':
      return t('stopped: time limit reached')
    case 'no_remaining_channel':
      return t('stopped: no allowed channel left')
    case 'strict_session':
      return t('stopped: strict session binding')
    default:
      return decision
  }
}
