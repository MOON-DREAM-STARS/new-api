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
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'

import type { WebProject } from '../types'

type ProjectDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  projects: WebProject[]
  isLoading: boolean
  errorMessageKey: string | null
  selectedProjectId: number | null
  canCreate: boolean
  removalNames: string[]
  onDismissRemoval: () => void
  onRefetch: () => void
  onSelect: (project: WebProject) => void
  onCreate: () => void
  onRename: (project: WebProject) => void
  onDelete: (project: WebProject) => void
}

/**
 * Full project management, opened on demand. It overlays the remote browser
 * instead of permanently narrowing it: the rail stays 64px wide and this
 * drawer carries the complete names and the real create/rename/delete flows.
 */
export function ProjectDrawer(props: ProjectDrawerProps) {
  const { t } = useTranslation()

  function renderList(): React.ReactNode {
    if (props.isLoading) {
      return (
        <p className='text-muted-foreground flex items-center gap-2 text-sm'>
          <Spinner className='size-4 motion-reduce:animate-none' />
          {t('Loading projects...')}
        </p>
      )
    }
    if (props.errorMessageKey) {
      return (
        <Alert variant='destructive' role='alert'>
          <AlertTitle>{t('Could not load projects')}</AlertTitle>
          <AlertDescription>{t(props.errorMessageKey)}</AlertDescription>
        </Alert>
      )
    }
    if (props.projects.length === 0) {
      return (
        <p className='text-muted-foreground text-sm'>
          {t(
            'No projects yet. Create one inside the remote browser and it is registered here automatically.'
          )}
        </p>
      )
    }
    return (
      <ul className='flex flex-col gap-1.5'>
        {props.projects.map((project) => (
          <li
            key={project.id}
            className={cn(
              'flex items-center gap-2 rounded-lg border px-2 py-1.5',
              project.id === props.selectedProjectId
                ? 'border-primary/40 bg-primary/5'
                : 'border-transparent'
            )}
          >
            <button
              type='button'
              className='min-w-0 flex-1 truncate text-left text-sm'
              aria-pressed={project.id === props.selectedProjectId}
              onClick={() => {
                props.onSelect(project)
              }}
            >
              {project.name}
            </button>
            <Button
              type='button'
              size='icon-sm'
              variant='ghost'
              aria-label={t('Rename {{name}}', { name: project.name })}
              onClick={() => {
                props.onRename(project)
              }}
            >
              <Pencil aria-hidden='true' />
            </Button>
            <Button
              type='button'
              size='icon-sm'
              variant='ghost'
              aria-label={t('Delete {{name}}', { name: project.name })}
              onClick={() => {
                props.onDelete(project)
              }}
            >
              <Trash2 aria-hidden='true' />
            </Button>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side='right'
        className='flex w-full flex-col gap-3 sm:max-w-sm'
      >
        <SheetHeader>
          <SheetTitle>{t('Projects')}</SheetTitle>
          <SheetDescription>
            {t(
              'Projects are registered by the browser guard when they are created inside the remote browser.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='flex items-center gap-2'>
          <Button
            type='button'
            size='sm'
            disabled={!props.canCreate}
            onClick={props.onCreate}
          >
            <Plus aria-hidden='true' />
            {t('New project')}
          </Button>
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            aria-label={t('Refresh projects')}
            aria-busy={props.isLoading}
            disabled={props.isLoading}
            onClick={props.onRefetch}
          >
            <RefreshCw aria-hidden='true' />
          </Button>
        </div>

        {props.removalNames.length > 0 ? (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>{t('Resource unavailable or removed')}</AlertTitle>
            <AlertDescription>
              {t(
                '{{names}} is no longer registered. Start the registration flow again to re-register it.',
                { names: props.removalNames.join(', ') }
              )}
            </AlertDescription>
            <div className='flex gap-2'>
              <Button type='button' size='sm' onClick={props.onCreate}>
                {t('Start registration flow')}
              </Button>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={props.onDismissRemoval}
              >
                {t('Dismiss')}
              </Button>
            </div>
          </Alert>
        ) : null}

        <div className='min-h-0 flex-1 overflow-y-auto'>{renderList()}</div>
      </SheetContent>
    </Sheet>
  )
}
