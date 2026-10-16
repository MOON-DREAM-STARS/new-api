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
import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { cn } from '@/lib/utils'

import type { ApiKeyFormValues } from '../../lib'
import { ExpirationField } from './expiration-field'

type OptionsStepProps = {
  form: UseFormReturn<ApiKeyFormValues>
  /** Quantity only applies when creating keys. */
  isUpdate: boolean
}

const switchRowClassName =
  'flex flex-row items-center justify-between gap-3 rounded-lg border p-3'

function QuotaFields(props: { form: UseFormReturn<ApiKeyFormValues> }) {
  const { t } = useTranslation()
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const unlimitedQuota = props.form.watch('unlimited_quota')
  return (
    <>
      {!unlimitedQuota && (
        <FormField
          control={props.form.control}
          name='remain_quota_dollars'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('Quota ({{currency}})', { currency: currencyLabel })}
              </FormLabel>
              <FormControl>
                <Input
                  {...field}
                  type='number'
                  step={tokensOnly ? 1 : 0.01}
                  placeholder={
                    tokensOnly
                      ? t('Enter quota in tokens')
                      : t('Enter quota in {{currency}}', {
                          currency: currencyLabel,
                        })
                  }
                  onChange={(e) =>
                    field.onChange(Number.parseFloat(e.target.value) || 0)
                  }
                />
              </FormControl>
              <FormDescription>
                {tokensOnly
                  ? t('Enter the quota amount in tokens')
                  : t('Enter the quota amount in {{currency}}', {
                      currency: currencyLabel,
                    })}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
      <FormField
        control={props.form.control}
        name='unlimited_quota'
        render={({ field }) => (
          <FormItem className={switchRowClassName}>
            <div className='flex flex-col gap-0.5'>
              <FormLabel className='text-sm'>{t('Unlimited Quota')}</FormLabel>
              <FormDescription className='text-xs'>
                {t('Enable unlimited quota for this API key')}
              </FormDescription>
            </div>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
          </FormItem>
        )}
      />
    </>
  )
}

/** Third page of the wizard: name, expiration, quantity, quota and IP limits. */
export function OptionsStep(props: OptionsStepProps) {
  const { t } = useTranslation()
  const [restrictionsOpen, setRestrictionsOpen] = useState(false)
  return (
    <div className='flex flex-col gap-4'>
      <FormField
        control={props.form.control}
        name='name'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Name')}</FormLabel>
            <FormControl>
              <Input {...field} placeholder={t('Enter a name')} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <ExpirationField form={props.form} />
      {!props.isUpdate && (
        <FormField
          control={props.form.control}
          name='tokenCount'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Quantity')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  type='number'
                  min='1'
                  placeholder={t('Number of keys to create')}
                  onChange={(e) =>
                    field.onChange(Number.parseInt(e.target.value, 10) || 1)
                  }
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Create multiple API keys at once (random suffix will be added to names)'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
      <QuotaFields form={props.form} />
      <Collapsible
        open={restrictionsOpen}
        onOpenChange={setRestrictionsOpen}
        className='rounded-lg border'
      >
        <CollapsibleTrigger className='hover:bg-muted/40 flex w-full items-center justify-between gap-3 rounded-lg p-3 text-start transition-colors'>
          <span className='min-w-0'>
            <span className='block text-sm font-medium'>
              {t('Access restrictions')}
            </span>
            <span className='text-muted-foreground block text-xs'>
              {t('Limit which IP addresses can use this API key')}
            </span>
          </span>
          <ChevronDown
            className={cn(
              'text-muted-foreground size-4 shrink-0 transition-transform',
              restrictionsOpen && 'rotate-180'
            )}
            aria-hidden='true'
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='px-3 pb-3'>
            <FormField
              control={props.form.control}
              name='allow_ips'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('IP Whitelist (supports CIDR)')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      className='min-h-20 resize-none'
                      placeholder={t(
                        'One IP per line (empty for no restriction)'
                      )}
                      rows={3}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Do not over-trust this feature. IP may be spoofed. Please use with nginx, CDN and other gateways.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
