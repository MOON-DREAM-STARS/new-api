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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Spinner } from '@/components/ui/spinner'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { modelsDevKeys, previewModelsDev } from './api'
import { isWritable } from './lib'
import type { ModelsDevOverview } from './types'

interface EnableDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  overview: ModelsDevOverview
  saving: boolean
  onConfirm: () => void
}

/** Explains what the automatic import does before it is switched on. */
export function EnableDialog(props: EnableDialogProps) {
  const { t } = useTranslation()
  const preview = useQuery({
    queryKey: [...modelsDevKeys.preview, 'enable'],
    queryFn: () => previewModelsDev(),
    enabled: props.open,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const plan = preview.data
  const waiting = (plan?.rows ?? []).filter(
    (row) => isWritable(row) && row.review
  ).length
  const runsSoon =
    props.overview.state.last_sync_version !== props.overview.current_version

  let summary
  if (preview.isError) {
    summary = (
      <p className='text-warning text-sm'>
        {t('models.dev could not be reached, so the effect cannot be shown.')}{' '}
        {getServerErrorMessage(preview.error)}
      </p>
    )
  } else if (!plan) {
    summary = (
      <p className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Spinner /> {t('Checking what would be imported...')}
      </p>
    )
  } else {
    summary = (
      <p className='bg-muted/50 rounded-lg border p-3 text-sm'>
        {t(
          'The next import would write {{created}} new prices and {{updated}} updates. {{locked}} models stay locked and {{waiting}} wait for your approval.',
          {
            created: plan.summary.created,
            updated: plan.summary.updated,
            locked: plan.summary.locked,
            waiting,
          }
        )}
      </p>
    )
  }

  return (
    <ConfirmDialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import prices from models.dev on every release?')}
      desc={
        <ul className='list-disc space-y-1.5 ps-5 text-start'>
          <li>
            {runsSoon
              ? t(
                  'The models you chose are imported once for this version shortly after you turn this on, and again at the first start of every new release.'
                )
              : t(
                  'The models you chose are imported at the first start of every new release.'
                )}
          </li>
          <li>
            {t(
              'A model that already has a price is locked and is never overwritten, unless you allow the import for it.'
            )}
          </li>
          <li>
            {t(
              'Prices of resellers and prices that moved by more than 10 times wait for your approval and are skipped.'
            )}
          </li>
          <li>{t('You can undo the last import at any time.')}</li>
        </ul>
      }
      confirmText={t('Turn on')}
      isLoading={props.saving}
      handleConfirm={props.onConfirm}
    >
      {summary}
    </ConfirmDialog>
  )
}
