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
import { Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'

import type { WebProject } from '../types'

type ProjectRowProps = {
  project: WebProject
  onRename: (project: WebProject) => void
  onDelete: (project: WebProject) => void
}

/** One registered project with its rename and delete actions. */
export function ProjectRow(props: ProjectRowProps) {
  const { t } = useTranslation()
  const updatedAt = dayjs
    .unix(props.project.updated_at)
    .format('YYYY-MM-DD HH:mm')

  return (
    <li className='flex items-start justify-between gap-3 rounded-lg border p-3'>
      <div className='min-w-0'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='truncate text-sm font-medium'>
            {props.project.name}
          </span>
          <Badge variant='outline'>{props.project.provider}</Badge>
        </div>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t('Updated at {{time}}', { time: updatedAt })}
        </p>
      </div>
      <div className='flex shrink-0 gap-1'>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          aria-label={t('Rename {{name}}', { name: props.project.name })}
          onClick={() => props.onRename(props.project)}
        >
          <Pencil aria-hidden='true' />
        </Button>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          aria-label={t('Delete {{name}}', { name: props.project.name })}
          onClick={() => props.onDelete(props.project)}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </div>
    </li>
  )
}
