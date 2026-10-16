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
import { Download } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { getModelsDevExpression } from './api'
import { EntryPickerDialog } from './entry-picker-dialog'
import type { ModelsDevCatalogItem } from './types'

interface ImportFromModelsDevProps {
  /** The model being edited; models.dev prices are written for this name. */
  modelName: string
  /** Receives the billing expression; the editor still has to be saved. */
  onExpression: (expression: string) => void
}

/** Fills the expression editor with the prices of one models.dev entry. */
export function ImportFromModelsDev(props: ImportFromModelsDevProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const name = props.modelName.trim()
  const pick = useMutation({
    mutationFn: (item: ModelsDevCatalogItem) =>
      getModelsDevExpression(item.key, name || item.normalized_id),
    onSuccess: (result) => {
      props.onExpression(result.expression)
      setOpen(false)
      toast.success(
        t('Filled in the prices of {{model}}. Review them, then save.', {
          model: result.entry.key,
        })
      )
    },
    onError: (error) =>
      handleServerError(error, t('Failed to import the prices')),
  })

  return (
    <div className='flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3'>
      <p className='text-muted-foreground min-w-0 flex-1 basis-60 text-xs'>
        {t(
          'Fill the expression with the prices listed on models.dev. Nothing is saved until you save the model.'
        )}
      </p>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => setOpen(true)}
      >
        <Download aria-hidden='true' />
        {t('Import from models.dev')}
      </Button>
      <EntryPickerDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Import from models.dev')}
        description={t(
          'Pick the models.dev model whose prices this model should use.'
        )}
        initialQuery={name}
        pickLabel={t('Use this price')}
        picking={pick.isPending}
        onPick={(item) => pick.mutate(item)}
      />
    </div>
  )
}
