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
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Spinner } from '@/components/ui/spinner'

import { useDeleteWebWorkspaceProject } from '../hooks/use-web-workspace-projects'
import { classifyWebWorkspaceError } from '../lib/errors'
import type { WebProject } from '../types'

type ProjectDeleteDialogProps = {
  project: WebProject | null
  onOpenChange: (open: boolean) => void
  /** True when this is the only registered project in the workspace. */
  isLastProject: boolean
  /** Opens the real creation flow without deleting the current project. */
  onCreate: () => void
  /** Called with the deleted project id so the removal is not reported as unexpected. */
  onDeleted: (projectId: number) => void
  /** Clears the expected-removal marker when the provider rejects the delete. */
  onDeleteRejected?: (projectId: number) => void
}

/** Confirmation dialog for deleting one provider-side project. */
export function ProjectDeleteDialog(props: ProjectDeleteDialogProps) {
  const { t } = useTranslation()
  const deleteMutation = useDeleteWebWorkspaceProject()
  const error = deleteMutation.error
    ? classifyWebWorkspaceError(deleteMutation.error)
    : null
  const lastProjectBlocked =
    props.isLastProject || error?.kind === 'last_project_required'

  const handleConfirm = async () => {
    const project = props.project
    if (!project) return
    // Mark the removal before the list updates so it is not reported as an
    // unexpected guard-side removal.
    props.onDeleted(project.id)
    try {
      await deleteMutation.mutateAsync(project.id)
      props.onOpenChange(false)
    } catch (failure) {
      if (classifyWebWorkspaceError(failure).kind === 'last_project_required') {
        props.onDeleteRejected?.(project.id)
      }
      // The mutation error is rendered in place.
    }
  }

  return (
    <AlertDialog
      open={Boolean(props.project)}
      onOpenChange={props.onOpenChange}
    >
      <AlertDialogContent>
        {lastProjectBlocked ? (
          <>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('Cannot delete the last project')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'At least one project must remain. Create another project before deleting this one.'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction
                onClick={(event) => {
                  event.preventDefault()
                  props.onOpenChange(false)
                  props.onCreate()
                }}
              >
                {t('New project')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </>
        ) : (
          <>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('Delete project {{name}}?', {
                  name: props.project?.name ?? '',
                })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'This deletes the provider-side project and its Web Workspace registration. This cannot be undone.'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            {error ? (
              <Alert variant='destructive' role='alert'>
                <AlertDescription>{t(error.messageKey)}</AlertDescription>
              </Alert>
            ) : null}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={deleteMutation.isPending}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={deleteMutation.isPending}
                onClick={(event) => {
                  event.preventDefault()
                  void handleConfirm()
                }}
              >
                {deleteMutation.isPending ? (
                  <Spinner className='size-4 motion-reduce:animate-none' />
                ) : null}
                {t('Delete project')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </>
        )}
      </AlertDialogContent>
    </AlertDialog>
  )
}
