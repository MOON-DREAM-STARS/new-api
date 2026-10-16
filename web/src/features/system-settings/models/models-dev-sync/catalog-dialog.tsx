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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Combobox } from '@/components/ui/combobox'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { useDebounce } from '@/hooks/use-debounce'

import { getModelsDevCatalog, modelsDevKeys } from './api'
import { EntryRow, ListState } from './catalog-entry'
import {
  choiceOf,
  isIncluded,
  sameChoice,
  setEntryIncluded,
  withChoice,
  type EntryChoice,
} from './lib'
import type { ModelsDevConfig } from './types'

const PAGE_SIZE = 40
const ALL_PROVIDERS = '__all__'

interface CatalogDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: ModelsDevConfig
  saving: boolean
  onSave: (config: ModelsDevConfig) => void
}

/** Chooses which models.dev entries the import takes. */
export function CatalogDialog(props: CatalogDialogProps) {
  const { t } = useTranslation()
  const { config } = props
  const [choice, setChoice] = useState<EntryChoice>(() => choiceOf(config))
  const [query, setQuery] = useState('')
  const [provider, setProvider] = useState(ALL_PROVIDERS)
  const [popularOnly, setPopularOnly] = useState(false)
  const [officialOnly, setOfficialOnly] = useState(false)
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [wasOpen, setWasOpen] = useState(props.open)
  if (props.open !== wasOpen) {
    setWasOpen(props.open)
    if (props.open) {
      setChoice(choiceOf(config))
      setLimit(PAGE_SIZE)
    }
  }
  const search = useDebounce(query.trim(), 300)
  const params = {
    q: search,
    provider: provider === ALL_PROVIDERS ? '' : provider,
    common: popularOnly,
    official: officialOnly,
    limit,
  }
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
  const providerOptions = useMemo(
    () => [
      { value: ALL_PROVIDERS, label: t('All providers') },
      ...(catalog.data?.providers ?? []).map((item) => ({
        value: item.id,
        label: `${item.name} (${item.count})`,
      })),
    ],
    [catalog.data?.providers, t]
  )
  const dirty = !sameChoice(choice, choiceOf(config))

  const setShown = (include: boolean) =>
    setChoice(
      items.reduce(
        (next, item) => setEntryIncluded(item, config, next, include),
        choice
      )
    )
  const filter = (apply: () => void) => {
    apply()
    setLimit(PAGE_SIZE)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Choose models to import')}
      description={t(
        'Models of your site and popular models are chosen by the switches on the page. Tick any other model, or untick one you do not want.'
      )}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(58vh, 34rem)'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={!dirty || props.saving}
            onClick={() => props.onSave(withChoice(config, choice))}
          >
            {props.saving ? <Spinner /> : null}
            {t('Save selection')}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-3'>
        <div className='flex flex-wrap items-center gap-3'>
          <InputGroup className='min-w-0 flex-1 basis-56'>
            <InputGroupAddon>
              <Search aria-hidden='true' />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(event) => filter(() => setQuery(event.target.value))}
              placeholder={t('Search models.dev by model or provider')}
              aria-label={t('Search models.dev by model or provider')}
            />
          </InputGroup>
          <Combobox
            options={providerOptions}
            value={provider}
            className='w-52'
            aria-label={t('Provider')}
            onValueChange={(next) =>
              filter(() => setProvider(next ?? ALL_PROVIDERS))
            }
          />
        </div>
        <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
          <Label className='flex items-center gap-2 text-sm font-normal'>
            <Switch
              size='sm'
              checked={popularOnly}
              onCheckedChange={(checked) =>
                filter(() => setPopularOnly(checked))
              }
            />
            {t('Popular models only')}
          </Label>
          <Label className='flex items-center gap-2 text-sm font-normal'>
            <Switch
              size='sm'
              checked={officialOnly}
              onCheckedChange={(checked) =>
                filter(() => setOfficialOnly(checked))
              }
            />
            {t('Official providers only')}
          </Label>
          <span className='flex-1' />
          <Button size='sm' variant='outline' onClick={() => setShown(true)}>
            {t('Select shown')}
          </Button>
          <Button size='sm' variant='outline' onClick={() => setShown(false)}>
            {t('Clear shown')}
          </Button>
        </div>

        <ListState query={catalog} empty={items.length === 0}>
          <p className='text-muted-foreground text-xs' role='status'>
            {t('{{count}} models', { count: total })}
            {' · '}
            {t('{{picked}} picked by hand, {{left}} left out', {
              picked: choice.selected.size,
              left: choice.excluded.size,
            })}
          </p>
          <ul className='space-y-2'>
            {items.map((item) => (
              <EntryRow
                key={item.key}
                item={item}
                leading={
                  <Checkbox
                    className='mt-1'
                    checked={isIncluded(item, config, choice)}
                    aria-label={item.key}
                    onCheckedChange={(checked) =>
                      setChoice(setEntryIncluded(item, config, choice, checked))
                    }
                  />
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
