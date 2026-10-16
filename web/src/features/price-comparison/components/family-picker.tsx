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
import { ArrowDown, ArrowUp, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'

import { isComparable } from '../lib/prices'
import type {
  PriceComparisonAdminFamily,
  PriceComparisonAdminModel,
} from '../types'

/** The most models one family lists on the page. */
export const MAX_MODELS_PER_FAMILY = 10

interface FamilyPickerProps {
  family: PriceComparisonAdminFamily
  /** Every model the administrator can see, by name. */
  models: ReadonlyMap<string, PriceComparisonAdminModel>
  /** The models of this family, in alphabetical order. */
  members: readonly PriceComparisonAdminModel[]
  /** The chosen models, in display order. */
  selected: readonly string[]
  disabled?: boolean
  onChange: (models: string[]) => void
}

type Problem = 'missing' | 'other_family' | 'not_comparable' | 'not_offered'

function problemOf(
  model: PriceComparisonAdminModel | undefined,
  familyId: number
): Problem | null {
  if (!model) return 'missing'
  if (model.family_id !== familyId) return 'other_family'
  if (!isComparable(model.price)) return 'not_comparable'
  if (!model.enabled) return 'not_offered'
  return null
}

function ProblemBadge(props: { problem: Problem | null }) {
  const { t } = useTranslation()
  switch (props.problem) {
    case 'missing':
      return <Badge variant='destructive'>{t('Model not found')}</Badge>
    case 'other_family':
      return (
        <Badge variant='destructive'>{t('Not in this model family')}</Badge>
      )
    case 'not_comparable':
      return <Badge variant='warning'>{t('Not comparable')}</Badge>
    case 'not_offered':
      return <Badge variant='warning'>{t('Not offered')}</Badge>
    default:
      return null
  }
}

/** Chooses and orders the models one model family lists on the page. */
export function FamilyPicker(props: FamilyPickerProps) {
  const { t } = useTranslation()
  const selected = props.selected
  const full = selected.length >= MAX_MODELS_PER_FAMILY

  const options: ComboboxInputOption[] = []
  for (const member of props.members) {
    if (selected.includes(member.model_name)) continue
    const comparable = isComparable(member.price)
    const option: ComboboxInputOption = {
      value: member.model_name,
      label: member.model_name,
      disabled: !comparable,
    }
    if (!comparable) option.description = t('Not comparable')
    else if (!member.enabled) option.description = t('Not offered')
    options.push(option)
  }

  const move = (index: number, offset: number) => {
    const next = [...selected]
    const target = index + offset
    if (target < 0 || target >= next.length) return
    ;[next[index], next[target]] = [next[target], next[index]]
    props.onChange(next)
  }

  return (
    <section
      aria-label={props.family.name}
      className='flex flex-col gap-2 rounded-xl border p-3'
    >
      <div className='flex items-center justify-between gap-2'>
        <h4 className='text-sm font-medium'>{props.family.name}</h4>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {selected.length}/{MAX_MODELS_PER_FAMILY}
        </span>
      </div>

      {selected.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t('No models chosen. This model family is hidden on the page.')}
        </p>
      ) : (
        <ul className='flex flex-col gap-1.5'>
          {selected.map((name, index) => (
            <li
              key={name}
              className='flex items-center gap-2 rounded-lg border px-2.5 py-1.5'
            >
              <span className='min-w-0 flex-1 font-mono text-sm break-all'>
                {name}
              </span>
              <ProblemBadge
                problem={problemOf(props.models.get(name), props.family.id)}
              />
              <div className='flex shrink-0 items-center'>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  disabled={props.disabled || index === 0}
                  aria-label={t('Move {{model}} up', { model: name })}
                  onClick={() => move(index, -1)}
                >
                  <ArrowUp />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  disabled={props.disabled || index === selected.length - 1}
                  aria-label={t('Move {{model}} down', { model: name })}
                  onClick={() => move(index, 1)}
                >
                  <ArrowDown />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  disabled={props.disabled}
                  aria-label={t('Remove {{model}}', { model: name })}
                  onClick={() =>
                    props.onChange(selected.filter((item) => item !== name))
                  }
                >
                  <X />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <Combobox
        options={options}
        value={null}
        disabled={props.disabled || full}
        placeholder={
          full
            ? t('You can list at most {{max}} models per model family', {
                max: MAX_MODELS_PER_FAMILY,
              })
            : t('Add a model')
        }
        emptyText={t('No models left to add')}
        aria-label={t('Add a model to {{family}}', {
          family: props.family.name,
        })}
        onValueChange={(name) => {
          if (name && !selected.includes(name)) {
            props.onChange([...selected, name])
          }
        }}
      />
    </section>
  )
}
