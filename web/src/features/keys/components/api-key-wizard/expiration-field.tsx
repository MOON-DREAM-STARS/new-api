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
import type { TFunction } from 'i18next'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ApiKeyFormValues } from '../../lib'

type ExpiryPreset = {
  id: 'never' | 'month' | 'day' | 'hour'
  months: number
  days: number
  hours: number
}

const EXPIRY_PRESETS: ExpiryPreset[] = [
  { id: 'never', months: 0, days: 0, hours: 0 },
  { id: 'month', months: 1, days: 0, hours: 0 },
  { id: 'day', months: 0, days: 1, hours: 0 },
  { id: 'hour', months: 0, days: 0, hours: 1 },
]

function presetLabel(t: TFunction, id: ExpiryPreset['id']): string {
  switch (id) {
    case 'never':
      return t('Never')
    case 'month':
      return t('1 Month')
    case 'day':
      return t('1 Day')
    case 'hour':
      return t('1 Hour')
  }
}

function expiryFromNow(preset: ExpiryPreset): Date | undefined {
  if (preset.months === 0 && preset.days === 0 && preset.hours === 0) {
    return undefined
  }
  const date = new Date()
  date.setMonth(date.getMonth() + preset.months)
  date.setDate(date.getDate() + preset.days)
  date.setHours(date.getHours() + preset.hours)
  return date
}

/** The expiration time with the usual presets next to the picker. */
export function ExpirationField(props: {
  form: UseFormReturn<ApiKeyFormValues>
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='expired_time'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Expiration Time')}</FormLabel>
          <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
            <FormControl>
              <DateTimePicker
                value={field.value}
                onChange={field.onChange}
                placeholder={t('Never expires')}
                className='min-w-0 [&_input[type=time]]:w-24 sm:[&_input[type=time]]:w-32'
              />
            </FormControl>
            <div className='grid grid-cols-4 gap-2 sm:flex'>
              {EXPIRY_PRESETS.map((preset) => (
                <Button
                  key={preset.id}
                  type='button'
                  variant='outline'
                  size='sm'
                  className='px-2 text-xs sm:px-3 sm:text-sm'
                  onClick={() =>
                    props.form.setValue('expired_time', expiryFromNow(preset), {
                      shouldDirty: true,
                    })
                  }
                >
                  {presetLabel(t, preset.id)}
                </Button>
              ))}
            </div>
          </div>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
