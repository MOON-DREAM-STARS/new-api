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
import { AppWindow, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'

import { useProjectRemovalNotice } from '../hooks/use-project-removal-notice'
import { useWebWorkspaceProjects } from '../hooks/use-web-workspace-projects'
import { classifyWebWorkspaceError } from '../lib/errors'
import type { WebProject } from '../types'
import { ProjectCreateDialog } from './project-create-dialog'
import { ProjectDeleteDialog } from './project-delete-dialog'
import { ProjectRenameDialog } from './project-rename-dialog'
import { ProjectRow } from './project-row'

/**
 * Remount key for the per-project dialogs so each dialog starts from the
 * selected project instead of the previous one.
 */
function projectDialogKey(prefix: string, project: WebProject | null): string {
  return [prefix, project ? String(project.id) : 'none'].join('-')
}

/**
 * Project registration list. Refreshing re-runs the server-side guard
 * synchronisation before the list is rendered, so the UI never shows a stale
 * registration the guard already dropped.
 */
export function ProjectPanel() {
  const { t } = useTranslation()
  const projectsQuery = useWebWorkspaceProjects()
  const removal = useProjectRemovalNotice(projectsQuery.data)
  const [createOpen, setCreateOpen] = useState(false)
  const [renameTarget, setRenameTarget] = useState<WebProject | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<WebProject | null>(null)

  const projects = projectsQuery.data ?? []
  const queryError = projectsQuery.isError
    ? classifyWebWorkspaceError(projectsQuery.error)
    : null

  let listBody: React.ReactNode
  if (projectsQuery.isPending) {
    listBody = (
      <p className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Spinner className='size-4 motion-reduce:animate-none' />
        {t('Loading projects...')}
      </p>
    )
  } else if (queryError) {
    listBody = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Could not load projects')}</AlertTitle>
        <AlertDescription>{t(queryError.messageKey)}</AlertDescription>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => {
            void projectsQuery.refetch()
          }}
        >
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else if (projects.length === 0) {
    listBody = (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <AppWindow aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('No projects yet.')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Create a project inside the remote browser and the system registers it here automatically.'
            )}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button type='button' onClick={() => setCreateOpen(true)}>
            {t('New project')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else {
    listBody = (
      <ul className='flex flex-col gap-2'>
        {projects.map((project) => (
          <ProjectRow
            key={project.id}
            project={project}
            onRename={setRenameTarget}
            onDelete={setDeleteTarget}
          />
        ))}
      </ul>
    )
  }
  return (
    <Card className='flex min-h-0 flex-1 flex-col'>
      <CardHeader className='flex-row items-center justify-between gap-2'>
        <CardTitle>{t('Projects')}</CardTitle>
        <div className='flex gap-1'>
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            aria-label={t('Refresh projects')}
            aria-busy={projectsQuery.isFetching}
            disabled={projectsQuery.isFetching}
            onClick={() => {
              void projectsQuery.refetch()
            }}
          >
            <RefreshCw aria-hidden='true' />
          </Button>
          <Button type='button' size='sm' onClick={() => setCreateOpen(true)}>
            {t('New project')}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='flex min-h-0 flex-1 flex-col gap-3 overflow-auto'>
        <Alert role='note'>
          <AlertDescription>
            {t(
              'Unregistered projects are rejected by the browser layer. A failed page load inside the remote browser is expected until the project is registered.'
            )}
          </AlertDescription>
        </Alert>

        {removal.removedNames.length > 0 ? (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>{t('Resource unavailable or removed')}</AlertTitle>
            <AlertDescription>
              {t(
                '{{names}} is no longer registered. Start the registration flow again to re-register it.',
                { names: removal.removedNames.join(', ') }
              )}
            </AlertDescription>
            <div className='flex gap-2'>
              <Button
                type='button'
                size='sm'
                onClick={() => setCreateOpen(true)}
              >
                {t('Start registration flow')}
              </Button>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={removal.dismiss}
              >
                {t('Dismiss')}
              </Button>
            </div>
          </Alert>
        ) : null}

        {listBody}
      </CardContent>

      <ProjectCreateDialog open={createOpen} onOpenChange={setCreateOpen} />
      <ProjectRenameDialog
        key={projectDialogKey('rename', renameTarget)}
        project={renameTarget}
        onOpenChange={(open) => {
          if (!open) setRenameTarget(null)
        }}
        onRemoved={removal.reportRemoved}
      />
      <ProjectDeleteDialog
        key={projectDialogKey('delete', deleteTarget)}
        project={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        onDeleted={removal.markExpectedRemoval}
      />
    </Card>
  )
}
