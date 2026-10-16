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

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import { draftProblem, normalizeCeiling, toggleTier } from '../lib/tiers'
import type { ServiceTierSettings, TierRatioRange } from '../types'
import { EscalationSettings } from './escalation-settings'
import { PreferenceOptions } from './preference-options'
import { TierSwitchCards } from './tier-switch-cards'

type ServiceTierSettingsEditorProps = {
  settings: ServiceTierSettings
  ratios: TierRatioRange
  /** Whose settings are edited: the account default or a single API key. */
  scope: 'account' | 'key'
  disabled?: boolean
  onChange: (settings: ServiceTierSettings) => void
}

/**
 * The tier switches, start preference and escalation rule. The service tiers
 * page edits the account default with it; the API key wizard edits the
 * settings of one key.
 */
export function ServiceTierSettingsEditor(
  props: ServiceTierSettingsEditorProps
) {
  const { t } = useTranslation()
  const problem = draftProblem(props.settings)
  const preferenceHint =
    props.scope === 'account'
      ? t(
          'Where each request starts. API keys without their own tier setting follow this preference.'
        )
      : t('Where each request of this API key starts.')
  return (
    <div className='min-w-0 space-y-4 sm:space-y-5'>
      <section aria-labelledby='service-tier-tiers' className='space-y-3'>
        <div className='space-y-1'>
          <h3 id='service-tier-tiers' className='text-sm font-semibold'>
            {t('Service tiers')}
          </h3>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Your requests keep the same model name and API key; the tiers you turn on decide which service level may serve them and how they are billed.'
            )}
          </p>
        </div>
        <TierSwitchCards
          enabledTiers={props.settings.enabled_tiers}
          ratios={props.ratios}
          disabled={props.disabled}
          onToggle={(tier, enabled) =>
            props.onChange(toggleTier(props.settings, tier, enabled))
          }
        />
      </section>
      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t('Default preference')}</CardTitle>
          <CardDescription>{preferenceHint}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-2'>
          <PreferenceOptions
            settings={props.settings}
            disabled={props.disabled}
            invalid={Boolean(problem)}
            onChange={(preference) =>
              props.onChange(
                normalizeCeiling({ ...props.settings, preference })
              )
            }
          />
          {problem && (
            <p role='alert' className='text-destructive text-xs'>
              {t(problem)}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t('Escalation')}</CardTitle>
          <CardDescription>
            {t(
              'Failover inside a tier is always on. Escalation to a pricier tier is optional and billed at the tier that serves the request.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <EscalationSettings
            settings={props.settings}
            ratios={props.ratios}
            disabled={props.disabled}
            onAllowChange={(allow) =>
              props.onChange(
                normalizeCeiling({ ...props.settings, allow_escalation: allow })
              )
            }
            onCeilingChange={(tier) =>
              props.onChange({ ...props.settings, max_escalation_tier: tier })
            }
          />
        </CardContent>
      </Card>
    </div>
  )
}
