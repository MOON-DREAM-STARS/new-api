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
import { useEffect, useRef, useState } from 'react'
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

import { fetchWebWorkspaceProjects, type WebWorkspaceScreenSize } from '../api'
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
import {
  composeProjectName,
  isValidProjectNameSegment,
  PROJECT_NAME_SEGMENT_MAX_LENGTH,
} from '../lib/project-name'
import { isLiveSessionState } from '../lib/session'
import type { WebProject } from '../types'

type ProjectCreateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** First-project mode cannot be dismissed before a project exists. */
  required?: boolean
  /** Screen size used when the dialog starts a session on the operator's behalf. */
  screenSize?: WebWorkspaceScreenSize | null
  onCreated?: (project: WebProject) => void
}

function findCreatedProject(
  projects: WebProject[],
  baselineIds: number[],
  expectedName: string
): WebProject | null {
  const baseline = new Set(baselineIds)
  const added = projects.filter((project) => !baseline.has(project.id))
  const exact = added.find((project) => project.name === expectedName)
  if (exact) return exact
  return added.length === 1 ? added[0] : null
}

/**
 * Project creation starts with a real permit. The guard then operates the
 * provider UI and the dialog follows the real session state until the guard
 * either registers the project or reports a terminal failure.
 */
export function ProjectCreateDialog(props: ProjectCreateDialogProps) {
  const { t } = useTranslation()
  const { open, onOpenChange, required = false } = props
  const queryClient = useQueryClient()
  const permitMutation = useCreateWebWorkspaceProjectPermit()
  const startSessionMutation = useStartWebWorkspaceSession()
  const [userName, setUserName] = useState('')
  const [projectName, setProjectName] = useState('')
  const [userNameTouched, setUserNameTouched] = useState(false)
  const [projectNameTouched, setProjectNameTouched] = useState(false)
  const [observedPermitId, setObservedPermitId] = useState<string | null>(null)
  const [resolvingCreated, setResolvingCreated] = useState(false)
  const [resolveError, setResolveError] = useState(false)
  const [resolveRetryToken, setResolveRetryToken] = useState(0)
  const baselineProjectIds = useRef<number[]>([])
  const resolveAttemptsRef = useRef(0)
  const resolvedPermitIdRef = useRef<string | null>(null)
  const onCreatedRef = useRef(props.onCreated)
  onCreatedRef.current = props.onCreated

  const permit = permitMutation.data ?? null
  const sessionQuery = useWebWorkspaceSession({ enabled: open })
  const sessionLive = isLiveSessionState(sessionQuery.data?.state)
  const projectCreation = sessionQuery.data?.project_creation ?? null
  const sessionRunning = projectCreation?.state === 'RUNNING'
  const matchingCreation =
    permit && projectCreation?.permit_id === permit.permit_id
      ? projectCreation
      : null
  const trackedCreation =
    matchingCreation ??
    (projectCreation?.permit_id === observedPermitId ? projectCreation : null)
  const creating = Boolean(permit) || sessionRunning
  const projectsQuery = useWebWorkspaceProjects(open && creating)
  const creationFailed = trackedCreation?.state === 'FAILED'
  const creationError = creationFailed ? (trackedCreation.error ?? '') : ''
  const combinedName = composeProjectName(userName, projectName)
  const userNameValid = isValidProjectNameSegment(userName)
  const projectNameValid = isValidProjectNameSegment(projectName)
  const isNameValid = userNameValid && projectNameValid
  const showUserNameError = userNameTouched && !userNameValid
  const showProjectNameError = projectNameTouched && !projectNameValid
  const mutationError = permitMutation.error ?? startSessionMutation.error
  const rawError = mutationError
    ? classifyWebWorkspaceError(mutationError)
    : null
  const conflictInProgress = rawError?.kind === 'project_creation_in_progress'
  const error = conflictInProgress ? null : rawError
  const sessionChecking = open && sessionQuery.isLoading && !permit
  const checkingCreation =
    conflictInProgress && (sessionQuery.isLoading || sessionQuery.isFetching)
  const isBusy =
    permitMutation.isPending ||
    startSessionMutation.isPending ||
    sessionChecking ||
    resolvingCreated
  const showCreateAction =
    !permit &&
    !error &&
    !creating &&
    !checkingCreation &&
    !creationFailed &&
    !resolvingCreated &&
    !resolveError

  useEffect(() => {
    if (!open || !sessionRunning || !projectCreation) return
    setObservedPermitId(projectCreation.permit_id)
  }, [open, projectCreation, sessionRunning])

  useEffect(() => {
    if (open) return
    setObservedPermitId(null)
    resolvedPermitIdRef.current = null
    resolveAttemptsRef.current = 0
    setResolvingCreated(false)
    setResolveError(false)
  }, [open])

  useEffect(() => {
    if (!open || trackedCreation?.state !== 'CREATED') return
    const permitId = trackedCreation.permit_id
    if (resolvedPermitIdRef.current === permitId) return
    if (resolveAttemptsRef.current >= 5) {
      setResolvingCreated(false)
      setResolveError(true)
      return
    }

    resolveAttemptsRef.current += 1
    setResolvingCreated(true)
    setResolveError(false)
    void (async () => {
      try {
        let projects = projectsQuery.data ?? []
        let created = findCreatedProject(
          projects,
          baselineProjectIds.current,
          combinedName
        )
        if (!created) {
          projects = await fetchWebWorkspaceProjects()
          queryClient.setQueryData(WEB_WORKSPACE_PROJECTS_QUERY_KEY, projects)
          created = findCreatedProject(
            projects,
            baselineProjectIds.current,
            combinedName
          )
        }
        if (!created) {
          if (resolveAttemptsRef.current < 5) {
            setResolveRetryToken((token) => token + 1)
          } else {
            setResolveError(true)
          }
          return
        }
        resolvedPermitIdRef.current = permitId
        resolveAttemptsRef.current = 0
        permitMutation.reset()
        setObservedPermitId(null)
        onCreatedRef.current?.(created)
        onOpenChange(false)
      } catch {
        if (resolveAttemptsRef.current < 5) {
          setResolveRetryToken((token) => token + 1)
        } else {
          setResolveError(true)
        }
      } finally {
        setResolvingCreated(false)
      }
    })()
  }, [
    combinedName,
    onOpenChange,
    open,
    permitMutation,
    projectsQuery.data,
    queryClient,
    resolveRetryToken,
    trackedCreation,
  ])

  useEffect(() => {
    if (!open || !conflictInProgress) return
    void queryClient.invalidateQueries({
      queryKey: WEB_WORKSPACE_SESSION_QUERY_KEY,
    })
  }, [conflictInProgress, open, queryClient])

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

  const resetForm = () => {
    permitMutation.reset()
    startSessionMutation.reset()
    setUserName('')
    setProjectName('')
    setUserNameTouched(false)
    setProjectNameTouched(false)
    setObservedPermitId(null)
    setResolvingCreated(false)
    setResolveError(false)
    resolvedPermitIdRef.current = null
    resolveAttemptsRef.current = 0
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && required) return
    if (!nextOpen) resetForm()
    onOpenChange(nextOpen)
  }

  const requestPermit = async () => {
    if (!isNameValid || isBusy || creationFailed) return
    baselineProjectIds.current = (projectsQuery.data ?? []).map(
      (project) => project.id
    )
    setResolveError(false)
    resolvedPermitIdRef.current = null
    resolveAttemptsRef.current = 0
    try {
      if (!sessionLive) {
        await startSessionMutation.mutateAsync({
          screenSize: props.screenSize ?? undefined,
        })
      }
      permitMutation.mutate({ name: combinedName })
    } catch {
      // The mutation error is rendered in place.
    }
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
            void requestPermit()
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
          onClick={() => {
            void requestPermit()
          }}
        >
          {permitMutation.isPending ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : null}
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else if (creationFailed) {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>{t('Automatic project creation failed')}</AlertTitle>
        <AlertDescription>
          {t(
            'Please contact an administrator to manually enable project creation permission.'
          )}
        </AlertDescription>
        <p className='text-muted-foreground font-mono text-xs'>
          {t('Error code: {{error}}', {
            error: creationError || t('Unknown'),
          })}
        </p>
      </Alert>
    )
  } else if (resolveError) {
    body = (
      <Alert variant='destructive' role='alert'>
        <AlertTitle>
          {t('Project was created but is not registered yet')}
        </AlertTitle>
        <AlertDescription>
          {t('Refresh the project list and try again.')}
        </AlertDescription>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => {
            resolveAttemptsRef.current = 0
            resolvedPermitIdRef.current = null
            setResolveError(false)
            setResolveRetryToken((token) => token + 1)
          }}
        >
          {t('Refresh project list')}
        </Button>
      </Alert>
    )
  } else if (resolvingCreated) {
    body = (
      <div
        role='status'
        className='text-muted-foreground flex items-center gap-3 rounded-lg border px-3 py-3 text-sm'
      >
        <Spinner className='size-4 shrink-0 motion-reduce:animate-none' />
        {t('Registering the created project...')}
      </div>
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
  } else if (checkingCreation) {
    body = (
      <div
        role='status'
        className='text-muted-foreground flex items-center gap-3 rounded-lg border px-3 py-3 text-sm'
      >
        <Spinner className='size-4 shrink-0 motion-reduce:animate-none' />
        {t('Checking the existing project creation...')}
      </div>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md' showCloseButton={!required}>
        <DialogHeader>
          <DialogTitle>
            {required ? t('Create your first project') : t('New project')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Enter a user identifier and project name. The system creates the project automatically in the remote browser.'
            )}
          </DialogDescription>
        </DialogHeader>

        <form
          noValidate
          className='grid gap-4'
          onSubmit={(event) => {
            event.preventDefault()
            void requestPermit()
          }}
        >
          <div className='grid gap-2'>
            <Label htmlFor='web-workspace-project-user-name'>
              {t('User name')}
            </Label>
            <Input
              id='web-workspace-project-user-name'
              autoFocus
              maxLength={PROJECT_NAME_SEGMENT_MAX_LENGTH}
              value={userName}
              aria-invalid={showUserNameError}
              disabled={
                permitMutation.isPending ||
                startSessionMutation.isPending ||
                creating ||
                creationFailed ||
                resolvingCreated
              }
              onChange={(event) => {
                setUserNameTouched(true)
                setUserName(event.target.value)
              }}
            />
            {showUserNameError ? (
              <p role='alert' className='text-destructive text-xs'>
                {t(
                  'Enter a user name of 1-24 letters, numbers, or underscores.'
                )}
              </p>
            ) : null}
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='web-workspace-project-name'>
              {t('Project name')}
            </Label>
            <Input
              id='web-workspace-project-name'
              maxLength={PROJECT_NAME_SEGMENT_MAX_LENGTH}
              value={projectName}
              aria-invalid={showProjectNameError}
              disabled={
                permitMutation.isPending ||
                startSessionMutation.isPending ||
                creating ||
                creationFailed ||
                resolvingCreated
              }
              onChange={(event) => {
                setProjectNameTouched(true)
                setProjectName(event.target.value)
              }}
            />
            {showProjectNameError ? (
              <p role='alert' className='text-destructive text-xs'>
                {t(
                  'Enter a project name of 1-24 letters, numbers, or underscores.'
                )}
              </p>
            ) : null}
          </div>

          <p className='text-muted-foreground text-xs'>
            {t('Final project name: {{name}}', {
              name: userName || projectName ? combinedName : '—',
            })}
          </p>

          {body}

          {showCreateAction ? (
            <DialogFooter>
              <Button type='submit' disabled={!isNameValid || isBusy}>
                {permitMutation.isPending ? (
                  <Spinner className='size-4 motion-reduce:animate-none' />
                ) : null}
                {required ? t('Create first project') : t('Create project')}
              </Button>
            </DialogFooter>
          ) : null}
        </form>
      </DialogContent>
    </Dialog>
  )
}
