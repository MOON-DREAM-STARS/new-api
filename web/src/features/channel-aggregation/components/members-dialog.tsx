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
import { Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import { updateFamilyMembers } from '../api'
import type { AggregationModel, ModelFamily } from '../types'

type MembersDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  family: ModelFamily
  families: ModelFamily[]
  models: AggregationModel[]
  overrideModels: string[]
  onSaved: () => void
}

export function MembersDialog(props: MembersDialogProps) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  useEffect(() => {
    if (props.open) {
      setSelected(props.family.members)
      setSearch('')
    }
  }, [props.open, props.family.members])

  const familyNames = useMemo(
    () => new Map(props.families.map((family) => [family.id, family.name])),
    [props.families]
  )
  const patterns = props.family.match_patterns
  const rows = useMemo(() => {
    const term = search.trim().toLowerCase()
    return props.models
      .map((model) => {
        const name = model.name.toLowerCase()
        const otherFamily =
          model.family_id !== 0 && model.family_id !== props.family.id
        return {
          ...model,
          otherFamily,
          suggested:
            !otherFamily && patterns.some((pattern) => name.includes(pattern)),
        }
      })
      .filter((model) => !term || model.name.toLowerCase().includes(term))
      .sort(
        (a, b) =>
          Number(b.suggested) - Number(a.suggested) ||
          a.name.localeCompare(b.name)
      )
  }, [props.models, props.family.id, patterns, search])
  const suggestions = rows.filter(
    (row) => row.suggested && !selected.includes(row.name)
  )
  const removedOverrides = props.overrideModels.filter(
    (model) => !selected.includes(model)
  )

  const save = useMutation({
    mutationFn: () =>
      updateFamilyMembers(props.family.id, selected, props.family.version),
    onSuccess: (result) => {
      toast.success(
        result.removed_overrides > 0
          ? t('Models saved; {{count}} overrides were removed', {
              count: result.removed_overrides,
            })
          : t('Models saved')
      )
      setConfirmOpen(false)
      props.onOpenChange(false)
      props.onSaved()
    },
    onError: (error) => handleServerError(error, t('Failed to save')),
  })
  const submit = () =>
    removedOverrides.length > 0 ? setConfirmOpen(true) : save.mutate()

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Models of {{family}}', { family: props.family.name })}
        description={t(
          'Models come from your channels and model metadata. A model belongs to one family at most.'
        )}
        contentClassName='sm:max-w-2xl'
        contentHeight='min(60dvh, 560px)'
        footer={
          <>
            <span className='text-muted-foreground mr-auto self-center text-xs'>
              {t('{{count}} selected', { count: selected.length })}
            </span>
            <Button
              type='button'
              variant='outline'
              onClick={() => props.onOpenChange(false)}
              disabled={save.isPending}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={submit} disabled={save.isPending}>
              {save.isPending && <Spinner data-icon='inline-start' />}
              {t('Save')}
            </Button>
          </>
        }
      >
        <div className='space-y-3'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
            <div className='relative min-w-0 flex-1'>
              <Search
                className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2'
                aria-hidden='true'
              />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search models')}
                aria-label={t('Search models')}
                className='pl-8'
              />
            </div>
            <Button
              type='button'
              variant='outline'
              disabled={suggestions.length === 0}
              onClick={() =>
                setSelected((current) => [
                  ...current,
                  ...suggestions.map((row) => row.name),
                ])
              }
            >
              {t('Add {{count}} suggestions', { count: suggestions.length })}
            </Button>
          </div>
          {rows.length === 0 ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No matching models')}
            </p>
          ) : (
            <ul className='divide-y rounded-lg border' aria-label={t('Models')}>
              {rows.map((row) => {
                const id = `family-member-${row.name}`
                const checked = selected.includes(row.name)
                return (
                  <li
                    key={row.name}
                    className={cn(
                      'flex items-center gap-3 px-3 py-2',
                      row.otherFamily && 'opacity-60'
                    )}
                  >
                    <Checkbox
                      id={id}
                      checked={checked}
                      disabled={row.otherFamily}
                      onCheckedChange={(value) =>
                        setSelected((current) =>
                          value
                            ? [...current, row.name]
                            : current.filter((name) => name !== row.name)
                        )
                      }
                    />
                    <Label
                      htmlFor={id}
                      className='min-w-0 flex-1 cursor-pointer font-mono text-xs font-normal break-all'
                    >
                      {row.name}
                    </Label>
                    {row.suggested && !checked && (
                      <Badge variant='secondary'>{t('Suggested')}</Badge>
                    )}
                    {row.otherFamily && (
                      <Badge variant='outline'>
                        {t('In {{family}}', {
                          family: familyNames.get(row.family_id) ?? '',
                        })}
                      </Badge>
                    )}
                    {props.overrideModels.includes(row.name) && (
                      <Badge variant='outline'>{t('Override')}</Badge>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      </Dialog>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Remove models with overrides?')}
        desc={t(
          'These models have their own rules, which are deleted with them: {{models}}',
          {
            models: removedOverrides.join(', '),
          }
        )}
        destructive
        confirmText={t('Remove and save')}
        isLoading={save.isPending}
        handleConfirm={() => save.mutate()}
      />
    </>
  )
}
