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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'

import {
  WEB_WORKSPACE_PROJECT_CREATION_POLL_INTERVAL_MS,
  WEB_WORKSPACE_PROJECTS_QUERY_KEY,
  WEB_WORKSPACE_SESSION_QUERY_KEY,
} from '../constants'
import {
  useCreateWebWorkspaceProjectPermit,
  useWebWorkspaceProjects,
} from '../hooks/use-web-workspace-projects'
import {
  useStartWebWorkspaceSession,
  useWebWorkspaceSession,
} from '../hooks/use-web-workspace-session'
import { classifyWebWorkspaceError, isPolicyDenied } from '../lib/errors'

type ProjectCreateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const PROJECT_NAME_MAX_LENGTH = 64

function isValidProjectName(value: string): boolean {
  const length = [...value.trim()].length
  return length >= 1 && length <= PROJECT_NAME_MAX_LENGTH
}

/**
 * Project creation starts with a real permit. The guard then operates the
 * provider UI and the dialog follows the real session state until the guard
 * either registers the project or asks the operator to finish manually.
 */
export function ProjectCreateDialog(props: ProjectCreateDialogProps) {
  const { t } = useTranslation()
  const { open, onOpenChange } = props
  const queryClient = useQueryClient()
  const permitMutation = useCreateWebWorkspaceProjectPermit()
  const startSessionMutation = useStartWebWorkspaceSession()
  const [name, setName] = useState('')
  const [nameTouched, setNameTouched] = useState(false)

  const permit = permitMutation.data ?? null
  const creating = Boolean(permit)
  const sessionQuery = useWebWorkspaceSession({
    enabled: open && creating,
  })
  useWebWorkspaceProjects(open && creating)
  const projectCreation = sessionQuery.data?.project_creation ?? null
  const currentCreation =
    permit && projectCreation?.permit_id === permit.permit_id
      ? projectCreation
      : null
  const creationFailed = currentCreation?.state === 'FAILED'
  const creationError = creationFailed ? currentCreation.error : ''
  const trimmedName = name.trim()
  const isNameValid = isValidProjectName(name)
  // The field starts empty, so the hint only appears after the operator has
  // interacted with it instead of on open.
  const showNameError = nameTouched && !isNameValid
  const error = permitMutation.error
    ? classifyWebWorkspaceError(permitMutation.error)
    : null
  const isBusy = permitMutation.isPending || startSessionMutation.isPending

  useEffect(() => {
    if (!open || currentCreation?.state !== 'CREATED') return
    void queryClient.invalidateQueries({
      queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
    })
    permitMutation.reset()
    onOpenChange(false)
  }, [currentCreation, onOpenChange, open, permitMutation, queryClient])

  useEffect(() => {
    if (!open || !creating) return undefined
    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({
        queryKey: WEB_WORKSPACE_SESSION_QUERY_KEY,
      })
      void queryClient.invalidateQueries({
        queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
      })
    }, WEB_WORKSPACE_PROJECT_CREATION_POLL_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [creating, open, queryClient])

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      permitMutation.reset()
      startSessionMutation.reset()
      setName('')
      setNameTouched(false)
    }
    onOpenChange(open)
  }

  const requestPermit = () => {
    if (!isNameValid) return
    permitMutation.mutate({ name: trimmedName })
  }

  let body: React.ReactNode = null
  if (error && error.kind === 'session_required') {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Start a browser session first')}</AlertTitle>
        <AlertDescription>{t(error.messageKey)}</AlertDescription>
        <Button
          type='button'
          size='sm'
          disabled={isBusy}
          onClick={() => {
            startSessionMutation.mutate(undefined, {
              onSuccess: () => {
                permitMutation.reset()
              },
            })
          }}
        >
          {startSessionMutation.isPending ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : null}
          {t('Start session')}
        </Button>
      </Alert>
    )
  } else if (error && error.kind === 'project_limit') {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Project limit reached')}</AlertTitle>
        <AlertDescription>{t(error.messageKey)}</AlertDescription>
      </Alert>
    )
  } else if (error) {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>
          {isPolicyDenied(error)
            ? t('Resource unavailable or removed')
            : t('Could not issue a creation permit')}
        </AlertTitle>
        <AlertDescription>{t(error.messageKey)}</AlertDescription>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={!isNameValid || isBusy}
          onClick={requestPermit}
        >
          {permitMutation.isPending ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : null}
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else if (permit && creationFailed) {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Automatic project creation failed')}</AlertTitle>
        <AlertDescription>
          {t(
            'Create the project manually in the side panel. The remote browser is showing the full window; the system registers it automatically once the guard observes it.'
          )}
        </AlertDescription>
        <p className='text-muted-foreground font-mono text-xs'>
          {t('Error code: {{error}}', {
            error: creationError || t('Unknown'),
          })}
        </p>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={!isNameValid || isBusy}
          onClick={requestPermit}
        >
          {permitMutation.isPending ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : null}
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else if (creating) {
    body = (
      <div
        role='status'
        className='text-muted-foreground flex items-center gap-3 rounded-lg border px-3 py-3 text-sm'
      >
        <Spinner className='size-4 shrink-0 motion-reduce:animate-none' />
        {t('Creating the project in the remote browser...')}
      </div>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('New project')}</DialogTitle>
          <DialogDescription>
            {t(
              'Enter a name and the system creates it automatically in the remote browser.'
            )}
          </DialogDescription>
        </DialogHeader>

        <form
          noValidate
          className='grid gap-4'
          onSubmit={(event) => {
            event.preventDefault()
            requestPermit()
          }}
        >
          <div className='grid gap-2'>
            <Label htmlFor='web-workspace-project-name'>
              {t('Project name')}
            </Label>
            <Input
              id='web-workspace-project-name'
              autoFocus
              maxLength={PROJECT_NAME_MAX_LENGTH}
              value={name}
              aria-invalid={showNameError}
              aria-describedby={
                showNameError ? 'web-workspace-project-name-error' : undefined
              }
              disabled={creating && !creationFailed}
              onChange={(event) => {
                setNameTouched(true)
                setName(event.target.value)
              }}
            />
            {showNameError ? (
              <p
                id='web-workspace-project-name-error'
                role='alert'
                className='text-destructive text-xs'
              >
                {t('Enter a project name of 1-64 characters.')}
              </p>
            ) : null}
          </div>

          {body}

          {!permit && !error ? (
            <DialogFooter>
              <Button type='submit' disabled={!isNameValid || isBusy}>
                {permitMutation.isPending ? (
                  <Spinner className='size-4 motion-reduce:animate-none' />
                ) : null}
                {t('Create project')}
              </Button>
            </DialogFooter>
          ) : null}
        </form>
      </DialogContent>
    </Dialog>
  )
}
