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
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import {
  selectableFamilyIds,
  UNCLASSIFIED_FAMILY_ID,
} from '../../lib/model-families'
import type { UserModelFamilies } from '../../types'

type FamilyCardData = {
  id: number
  name: string
  description: string
  models: string[]
}

type FamilyStepProps = {
  data: UserModelFamilies | undefined
  loading: boolean
  /** Shown when the families could not be loaded. */
  errorMessage?: string
  /** The family ids the key is limited to now; empty for a key without limit. */
  storedIds: readonly number[]
  selection: ReadonlySet<number>
  disabled?: boolean
  onChange: (selection: ReadonlySet<number>) => void
  onRetry: () => void
}

function FamilyCard(props: {
  card: FamilyCardData
  checked: boolean
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const [open, setOpen] = useState(false)
  const count = props.card.models.length
  return (
    <li
      className={cn(
        'bg-card flex min-w-0 flex-col gap-2 rounded-xl border p-3 transition-colors',
        props.checked && 'border-primary/40 bg-primary/[0.03]'
      )}
    >
      <div className='flex items-start gap-3'>
        <Checkbox
          id={`${id}-check`}
          checked={props.checked}
          disabled={props.disabled}
          aria-describedby={`${id}-description`}
          className='mt-0.5'
          onCheckedChange={(checked) => props.onCheckedChange(Boolean(checked))}
        />
        <div className='min-w-0 flex-1 space-y-1'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <Label
              htmlFor={`${id}-check`}
              className='min-w-0 cursor-pointer text-sm font-semibold'
            >
              <span className='truncate'>{props.card.name}</span>
            </Label>
            <Badge variant={count > 0 ? 'secondary' : 'outline'}>
              {count > 0
                ? t('{{count}} models', { count })
                : t('No models yet')}
            </Badge>
          </div>
          <p
            id={`${id}-description`}
            className='text-muted-foreground text-xs leading-relaxed'
          >
            {props.card.description}
          </p>
        </div>
      </div>
      {count > 0 && (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='xs'
                className='text-muted-foreground -ml-1'
              />
            }
          >
            {open ? t('Hide models') : t('Show models')}
            <ChevronDown
              className={cn('transition-transform', open && 'rotate-180')}
              aria-hidden='true'
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className='mt-2 flex max-h-28 flex-wrap gap-1 overflow-y-auto'>
              {props.card.models.map((name) => (
                <li key={name} className='max-w-full'>
                  <Badge variant='outline' className='font-mono'>
                    <span className='truncate'>{name}</span>
                  </Badge>
                </li>
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
    </li>
  )
}

/** First page of the wizard: the model families the key may use. */
export function FamilyStep(props: FamilyStepProps) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      <div
        role='status'
        aria-label={t('Loading...')}
        className='grid gap-3 sm:grid-cols-2'
      >
        {[0, 1, 2, 3].map((index) => (
          <Skeleton key={index} className='h-24 w-full' />
        ))}
      </div>
    )
  }
  if (!props.data) {
    return (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load model families')}
        description={props.errorMessage}
        onRetry={props.onRetry}
      />
    )
  }

  const selectable = selectableFamilyIds(props.data, props.storedIds)
  const cards: FamilyCardData[] = props.data.families.map((family) => ({
    id: family.id,
    name: family.name,
    description: family.description || family.key,
    models: family.models,
  }))
  if (selectable.includes(UNCLASSIFIED_FAMILY_ID)) {
    cards.push({
      id: UNCLASSIFIED_FAMILY_ID,
      name: t('Other models'),
      description: t('Models that are not part of any family yet'),
      models: props.data.unclassified_models,
    })
  }
  const selectedCount = selectable.filter((id) =>
    props.selection.has(id)
  ).length
  const toggle = (id: number, checked: boolean) => {
    const next = new Set(props.selection)
    if (checked) next.add(id)
    else next.delete(id)
    props.onChange(next)
  }

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <p className='text-muted-foreground min-w-0 flex-1 basis-60 text-xs leading-relaxed'>
          {t(
            'Choose the model families this API key can use. Models added to a selected family later are available to the key automatically.'
          )}
        </p>
        <div className='flex shrink-0 items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled || selectedCount === selectable.length}
            onClick={() => props.onChange(new Set(selectable))}
          >
            {t('Select all')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled || selectedCount === 0}
            onClick={() => props.onChange(new Set())}
          >
            {t('Clear')}
          </Button>
        </div>
      </div>
      {cards.length === 0 && (
        <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm'>
          {t('No models are available to your account yet.')}
        </p>
      )}
      <ul className='grid gap-3 sm:grid-cols-2'>
        {cards.map((card) => (
          <FamilyCard
            key={card.id}
            card={card}
            checked={props.selection.has(card.id)}
            disabled={props.disabled}
            onCheckedChange={(checked) => toggle(card.id, checked)}
          />
        ))}
      </ul>
      <p
        aria-live='polite'
        className={cn(
          'text-xs',
          selectedCount === 0 && cards.length > 0
            ? 'text-destructive'
            : 'text-muted-foreground'
        )}
      >
        {selectedCount === 0 && cards.length > 0
          ? t('Select at least one model family')
          : t('{{selected}} of {{total}} model families selected', {
              selected: selectedCount,
              total: selectable.length,
            })}
      </p>
    </div>
  )
}
