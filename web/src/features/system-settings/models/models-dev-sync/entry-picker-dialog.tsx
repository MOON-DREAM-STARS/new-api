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
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useDebounce } from '@/hooks/use-debounce'

import { getModelsDevCatalog, modelsDevKeys } from './api'
import { EntryRow, ListState } from './catalog-entry'
import type { ModelsDevCatalogItem } from './types'

const PAGE_SIZE = 30

interface EntryPickerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  /** What the search box starts with, usually the name of the model. */
  initialQuery?: string
  pickLabel: string
  picking?: boolean
  onPick: (item: ModelsDevCatalogItem) => void
}

/** Searches models.dev and hands back the entry the administrator picked. */
export function EntryPickerDialog(props: EntryPickerDialogProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState(props.initialQuery ?? '')
  const [officialOnly, setOfficialOnly] = useState(false)
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [wasOpen, setWasOpen] = useState(props.open)
  if (props.open !== wasOpen) {
    setWasOpen(props.open)
    if (props.open) {
      setQuery(props.initialQuery ?? '')
      setLimit(PAGE_SIZE)
    }
  }
  const search = useDebounce(query.trim(), 300)
  const params = { q: search, official: officialOnly, limit }
  const catalog = useQuery({
    queryKey: modelsDevKeys.catalog(params),
    queryFn: () => getModelsDevCatalog(params),
    // Wait for the typing to settle before asking for a new list.
    enabled: props.open && search === query.trim(),
    placeholderData: keepPreviousData,
    meta: { errorToast: false },
  })
  const items = catalog.data?.items ?? []
  const total = catalog.data?.total ?? 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={props.title}
      description={props.description}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(60vh, 34rem)'
      footer={
        <Button variant='outline' onClick={() => props.onOpenChange(false)}>
          {t('Close')}
        </Button>
      }
    >
      <div className='flex flex-col gap-3'>
        <div className='flex flex-wrap items-center gap-3'>
          <InputGroup className='min-w-0 flex-1 basis-60'>
            <InputGroupAddon>
              <Search aria-hidden='true' />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setLimit(PAGE_SIZE)
              }}
              placeholder={t('Search models.dev by model or provider')}
              aria-label={t('Search models.dev by model or provider')}
            />
          </InputGroup>
          <Label className='flex items-center gap-2 text-sm font-normal'>
            <Switch
              size='sm'
              checked={officialOnly}
              onCheckedChange={(checked) => {
                setOfficialOnly(checked)
                setLimit(PAGE_SIZE)
              }}
            />
            {t('Official providers only')}
          </Label>
        </div>

        <ListState query={catalog} empty={items.length === 0}>
          <p className='text-muted-foreground text-xs' role='status'>
            {t('{{count}} models', { count: total })}
          </p>
          <ul className='space-y-2'>
            {items.map((item) => (
              <EntryRow
                key={item.key}
                item={item}
                trailing={
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={props.picking}
                    onClick={() => props.onPick(item)}
                    aria-label={`${props.pickLabel}: ${item.key}`}
                  >
                    {props.pickLabel}
                  </Button>
                }
              />
            ))}
          </ul>
          {items.length < total ? (
            <Button
              variant='ghost'
              className='self-center'
              disabled={catalog.isFetching}
              onClick={() => setLimit(limit + PAGE_SIZE)}
            >
              {t('Show more')}
            </Button>
          ) : null}
        </ListState>
      </div>
    </Dialog>
  )
}
