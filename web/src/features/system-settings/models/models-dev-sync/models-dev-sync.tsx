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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, MoreHorizontal, Undo2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemTitle,
} from '@/components/ui/item'
import { Switch } from '@/components/ui/switch'
import { invalidateModelPricing } from '@/features/model-pricing/api'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getModelsDevOverview,
  modelsDevKeys,
  saveModelsDevConfig,
  undoModelsDevSync,
} from './api'
import { CatalogDialog } from './catalog-dialog'
import { EnableDialog } from './enable-dialog'
import { SyncDialog } from './sync-dialog'
import type {
  ModelsDevConfig,
  ModelsDevOverview,
  ModelsDevSyncResult,
} from './types'

interface SwitchRowProps {
  title: string
  description: string
  checked: boolean
  disabled: boolean
  onChange: (checked: boolean) => void
  action?: ReactNode
}

function SwitchRow(props: SwitchRowProps) {
  return (
    <Item variant='outline'>
      <ItemContent>
        <ItemTitle>{props.title}</ItemTitle>
        <ItemDescription>{props.description}</ItemDescription>
      </ItemContent>
      <ItemActions>
        {props.action}
        <Switch
          checked={props.checked}
          disabled={props.disabled}
          onCheckedChange={props.onChange}
          aria-label={props.title}
        />
      </ItemActions>
    </Item>
  )
}

/** What the last import did, and the error it ended with, if any. */
function LastImport({ overview }: { overview: ModelsDevOverview }) {
  const { t } = useTranslation()
  const { state } = overview
  const summary = state.last_summary
  return (
    <div className='space-y-2'>
      {state.last_sync_at ? (
        <div className='text-sm'>
          <p>
            {t('Last import: {{time}} · version {{version}} · {{how}}', {
              time: formatTimestampToDate(state.last_sync_at),
              version: state.last_sync_version || '-',
              how:
                state.last_trigger === 'release'
                  ? t('new release')
                  : t('by hand'),
            })}
          </p>
          <p className='text-muted-foreground'>
            {t(
              '{{created}} new, {{updated}} updated, {{locked}} locked, {{skipped}} waiting for approval',
              {
                created: summary.created,
                updated: summary.updated,
                locked: summary.locked,
                skipped: summary.skipped_review,
              }
            )}
          </p>
        </div>
      ) : (
        <p className='text-muted-foreground text-sm'>{t('No import yet.')}</p>
      )}
      {state.last_error ? (
        <Alert variant='destructive'>
          <AlertTitle>{t('The last import failed')}</AlertTitle>
          <AlertDescription>
            {state.last_error}
            {state.last_error_at
              ? ` · ${formatTimestampToDate(state.last_error_at)}`
              : ''}
          </AlertDescription>
        </Alert>
      ) : null}
      {overview.auto_sync_due ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Version {{version}} has not imported yet. The next scheduled check imports it.',
            { version: overview.current_version }
          )}
        </p>
      ) : null}
    </div>
  )
}

type DialogName = 'catalog' | 'sync' | 'enable' | 'undo'

interface ModelsDevSyncProps {
  /** Called after prices changed, so the tables of the page load them again. */
  onPricesChanged?: () => void | Promise<void>
}

/** The "models.dev sync" tab: switches, last result, and the dialogs. */
export function ModelsDevSync(props: ModelsDevSyncProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialog, setDialog] = useState<DialogName | null>(null)
  const overview = useQuery({
    queryKey: modelsDevKeys.overview,
    queryFn: getModelsDevOverview,
    staleTime: 0,
    meta: { errorToast: false },
  })

  const refreshPrices = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: modelsDevKeys.all }),
      invalidateModelPricing(queryClient),
    ])
  const saveConfig = useMutation({
    mutationFn: saveModelsDevConfig,
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: modelsDevKeys.all }),
    onError: (error) => handleServerError(error, t('Failed to save')),
  })
  const save = async (config: ModelsDevConfig) => {
    try {
      await saveConfig.mutateAsync(config)
      return true
    } catch {
      return false
    }
  }
  const undo = useMutation({
    mutationFn: undoModelsDevSync,
    onSuccess: async (result) => {
      await refreshPrices()
      toast.success(
        t('Restored {{count}} models', { count: result.restored.length })
      )
      if (result.conflicts.length > 0) {
        toast.warning(
          t(
            '{{count}} models were edited since and keep their price: {{models}}',
            {
              count: result.conflicts.length,
              models: result.conflicts.join(', '),
            }
          )
        )
      }
      setDialog(null)
      void props.onPricesChanged?.()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to undo the import')),
  })
  const handleSynced = (result: ModelsDevSyncResult) => {
    toast.success(
      t('Imported {{count}} models from models.dev', {
        count: result.written.length,
      })
    )
    if (result.skipped_review.length > 0) {
      toast.info(
        t('{{count}} proposals still wait for your approval', {
          count: result.skipped_review.length,
        })
      )
    }
    void refreshPrices()
    void props.onPricesChanged?.()
  }

  if (overview.isError) {
    return (
      <ErrorState
        description={getServerErrorMessage(overview.error)}
        onRetry={() => void overview.refetch()}
      />
    )
  }
  if (!overview.data) return <LoadingState />
  const { config, state } = overview.data
  const busy = saveConfig.isPending
  const close = (open: boolean) => {
    if (!open) setDialog(null)
  }

  return (
    <div className='flex max-w-3xl flex-col gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Prices come from the public list on models.dev, in US dollars per million tokens. Each one is written as the billing expression of the model, so it can be edited like any other price.'
        )}
      </p>
      <SwitchRow
        title={t('Import on every new release')}
        description={t(
          'At the first start of a new version, the prices of the chosen models are imported once.'
        )}
        checked={config.auto_sync_enabled}
        disabled={busy}
        onChange={(checked) =>
          checked
            ? setDialog('enable')
            : void save({ ...config, auto_sync_enabled: false })
        }
      />
      <LastImport overview={overview.data} />
      <SwitchRow
        title={t('Models of your site')}
        description={t(
          'Import the models you offer when models.dev lists the same name.'
        )}
        checked={config.include_site_models}
        disabled={busy}
        onChange={(checked) =>
          void save({ ...config, include_site_models: checked })
        }
      />
      <SwitchRow
        title={t('Popular models')}
        description={t(
          'Also import the newest models of the popular families: Claude, GPT, Gemini, DeepSeek, Qwen, Kimi, GLM and more.'
        )}
        checked={config.include_common_models}
        disabled={busy}
        onChange={(checked) =>
          void save({ ...config, include_common_models: checked })
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <Button onClick={() => setDialog('sync')}>
          <Download aria-hidden='true' />
          {t('Import now')}
        </Button>
        <Button variant='outline' onClick={() => setDialog('catalog')}>
          {t('Choose models')}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant='ghost'
                size='icon'
                aria-label={t('More actions')}
              />
            }
          >
            <MoreHorizontal aria-hidden='true' />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='start' className='w-56'>
            <DropdownMenuItem
              disabled={state.undo_count === 0}
              onClick={() => setDialog('undo')}
            >
              <Undo2 aria-hidden='true' />
              {t('Undo the last import')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className='text-muted-foreground text-xs'>
          {t(
            '{{picked}} picked by hand · {{left}} left out · {{mapped}} mapped',
            {
              picked: config.selected_keys.length,
              left: config.excluded_keys.length,
              mapped: Object.keys(config.mappings).length,
            }
          )}
        </span>
      </div>

      <CatalogDialog
        open={dialog === 'catalog'}
        onOpenChange={close}
        config={config}
        saving={busy}
        onSave={async (next) => {
          if (await save(next)) setDialog(null)
        }}
      />
      <SyncDialog
        open={dialog === 'sync'}
        onOpenChange={close}
        config={config}
        onSaveConfig={save}
        onSynced={handleSynced}
      />
      <EnableDialog
        open={dialog === 'enable'}
        onOpenChange={close}
        overview={overview.data}
        saving={busy}
        onConfirm={async () => {
          if (await save({ ...config, auto_sync_enabled: true })) {
            setDialog(null)
          }
        }}
      />
      <ConfirmDialog
        open={dialog === 'undo'}
        onOpenChange={close}
        title={t('Undo the last import?')}
        desc={t(
          'The prices that import changed go back to what they were. A model whose price was edited since keeps its price.'
        )}
        confirmText={t('Undo import')}
        isLoading={undo.isPending}
        handleConfirm={() => undo.mutate()}
      >
        <p className='bg-muted/50 rounded-lg border p-3 text-sm'>
          {t('{{count}} models imported at {{time}}', {
            count: state.undo_count,
            time: formatTimestampToDate(state.undo_at),
          })}
        </p>
      </ConfirmDialog>
    </div>
  )
}
