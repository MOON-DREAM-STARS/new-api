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
import { Spinner } from '@/components/ui/spinner'

import { WEB_WORKSPACE_PROJECTS_QUERY_KEY } from '../constants'
import { useRemainingSeconds } from '../hooks/use-remaining-seconds'
import { useCreateWebWorkspaceProjectPermit } from '../hooks/use-web-workspace-projects'
import { useStartWebWorkspaceSession } from '../hooks/use-web-workspace-session'
import { classifyWebWorkspaceError, isPolicyDenied } from '../lib/errors'
import { formatCountdown } from '../lib/session'

type ProjectCreateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Project creation is provider-driven: the API only issues a short-lived
 * permit, the user creates the project inside the remote browser, and the
 * guard observation registers it. Every failure code gets an actionable hint.
 */
export function ProjectCreateDialog(props: ProjectCreateDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const permitMutation = useCreateWebWorkspaceProjectPermit()
  const startSessionMutation = useStartWebWorkspaceSession()

  const permit = permitMutation.data ?? null
  const remaining = useRemainingSeconds(permit?.expires_at)
  const isExpired = Boolean(permit) && remaining === 0
  const error = permitMutation.error
    ? classifyWebWorkspaceError(permitMutation.error)
    : null

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      permitMutation.reset()
      startSessionMutation.reset()
    }
    props.onOpenChange(open)
  }

  const refreshProjects = () => {
    queryClient.invalidateQueries({
      queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
    })
  }

  let body: React.ReactNode
  if (error && error.kind === 'session_required') {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Start a browser session first')}</AlertTitle>
        <AlertDescription>{t(error.messageKey)}</AlertDescription>
        <Button
          type='button'
          size='sm'
          disabled={startSessionMutation.isPending}
          onClick={() => {
            startSessionMutation.mutate(undefined, {
              onSuccess: () => {
                permitMutation.reset()
              },
            })
          }}
        >
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
          disabled={permitMutation.isPending}
          onClick={() => {
            permitMutation.mutate()
          }}
        >
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else if (!permit || isExpired) {
    body = (
      <>
        {isExpired ? (
          <Alert role='alert'>
            <AlertTitle>{t('Creation permit expired.')}</AlertTitle>
            <AlertDescription>
              {t('Issue a new permit and finish creating the project.')}
            </AlertDescription>
          </Alert>
        ) : null}
        <p className='text-muted-foreground text-sm'>
          {t(
            'A creation permit lets you register exactly one new project. Open the remote browser, create the project there, and the system registers it automatically.'
          )}
        </p>
        <Button
          type='button'
          disabled={permitMutation.isPending}
          onClick={() => {
            permitMutation.mutate()
          }}
        >
          {permitMutation.isPending ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : null}
          {isExpired ? t('Issue a new permit') : t('Issue creation permit')}
        </Button>
      </>
    )
  } else {
    body = (
      <>
        <Alert role='status'>
          <AlertTitle>
            {t('Permit expires in {{time}}.', {
              time: formatCountdown(remaining),
            })}
          </AlertTitle>
          <AlertDescription>
            {t(
              'Open the remote browser and create the project there. The system registers it automatically once the guard observes it.'
            )}
          </AlertDescription>
        </Alert>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={refreshProjects}>
            {t('Refresh project list')}
          </Button>
          <Button type='button' onClick={() => handleOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('Create a project in the remote browser')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'The provider creates the project; this dashboard only registers the result.'
            )}
          </DialogDescription>
        </DialogHeader>
        {body}
      </DialogContent>
    </Dialog>
  )
}
