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

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type WizardStepsProps = {
  /** Already translated step names. */
  labels: string[]
  current: number
  /** When set, a step can be opened by clicking it. */
  onSelect?: (index: number) => void
  /** Steps that cannot be opened yet, or while a request is running. */
  isDisabled?: (index: number) => boolean
}

/** The numbered steps at the top of the API key wizard. */
export function WizardSteps(props: WizardStepsProps) {
  const { t } = useTranslation()
  return (
    <ol
      aria-label={t('API key steps')}
      className={cn(
        'mb-4 grid gap-2',
        props.labels.length > 3 ? 'grid-cols-2 sm:grid-cols-4' : 'grid-cols-3'
      )}
    >
      {props.labels.map((label, index) => {
        const active = props.current === index
        const className = cn(
          'h-auto w-full min-w-0 justify-start rounded-lg border px-2 py-1.5 text-start text-xs font-normal sm:text-sm',
          active
            ? 'border-primary bg-primary/5 font-medium'
            : 'text-muted-foreground'
        )
        const content = (
          <>
            <span className='shrink-0'>{index + 1}.</span>
            <span className='truncate' title={label}>
              {label}
            </span>
          </>
        )
        return (
          <li key={label} aria-current={active ? 'step' : undefined}>
            {props.onSelect ? (
              <Button
                type='button'
                variant='ghost'
                disabled={props.isDisabled?.(index)}
                className={className}
                onClick={() => props.onSelect?.(index)}
              >
                {content}
              </Button>
            ) : (
              <div className={cn(className, 'flex items-center gap-1.5')}>
                {content}
              </div>
            )}
          </li>
        )
      })}
    </ol>
  )
}
