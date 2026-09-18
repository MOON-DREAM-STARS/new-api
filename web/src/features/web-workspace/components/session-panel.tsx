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
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'

import { useRemainingSeconds } from '../hooks/use-remaining-seconds'
import {
  useRestartWebWorkspaceSession,
  useStartWebWorkspaceSession,
  useStopWebWorkspaceSession,
  useWebWorkspaceSession,
} from '../hooks/use-web-workspace-session'
import { classifyWebWorkspaceError } from '../lib/errors'
import {
  formatCountdown,
  isLiveSessionState,
  sessionStateLabelKey,
} from '../lib/session'

/**
 * Session control panel: state, idle countdown and start/stop/restart.
 * The status line is an `aria-live` region so state changes are announced.
 */
export function SessionPanel() {
  const { t } = useTranslation()
  const sessionQuery = useWebWorkspaceSession()
  const startMutation = useStartWebWorkspaceSession()
  const stopMutation = useStopWebWorkspaceSession()
  const restartMutation = useRestartWebWorkspaceSession()
  const lastActionRef = useRef<(() => void) | null>(null)

  const session = sessionQuery.data ?? null
  const isLive = isLiveSessionState(session?.state)
  const remaining = useRemainingSeconds(session?.idle_deadline_at)
  const isBusy =
    startMutation.isPending ||
    stopMutation.isPending ||
    restartMutation.isPending

  const failedMutation =
    [startMutation, stopMutation, restartMutation].find(
      (mutation) => mutation.error
    ) ?? null
  const actionError = failedMutation
    ? classifyWebWorkspaceError(failedMutation.error)
    : null
  const queryError = sessionQuery.isError
    ? classifyWebWorkspaceError(sessionQuery.error)
    : null

  const runAction = (action: () => void) => {
    lastActionRef.current = action
    action()
  }

  const retry = () => {
    const lastAction = lastActionRef.current
    if (lastAction) {
      lastAction()
      return
    }
    void sessionQuery.refetch()
  }

  let statusText: string
  if (sessionQuery.isPending) {
    statusText = t('Loading browser session...')
  } else if (queryError) {
    statusText = t(queryError.messageKey)
  } else if (!session) {
    statusText = t('No browser session is running.')
  } else {
    statusText = t('Browser session is {{state}}.', {
      state: t(sessionStateLabelKey(session.state)),
    })
  }

  return (
    <Card>
      <CardHeader className='flex-row items-center justify-between gap-2'>
        <CardTitle>{t('Browser session')}</CardTitle>
        {session ? (
          <Badge variant={isLive ? 'default' : 'secondary'}>
            {t(sessionStateLabelKey(session.state))}
          </Badge>
        ) : null}
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        <p
          role='status'
          aria-live='polite'
          aria-atomic='true'
          className='text-muted-foreground text-sm'
        >
          {statusText}
        </p>

        {session && isLive && remaining > 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('Idle shutdown in {{time}}.', {
              time: formatCountdown(remaining),
            })}
          </p>
        ) : null}

        <div className='flex flex-wrap gap-2'>
          {!session || !isLive ? (
            <Button
              type='button'
              disabled={isBusy}
              onClick={() => {
                runAction(() => startMutation.mutate())
              }}
            >
              {startMutation.isPending ? (
                <Spinner className='size-4 motion-reduce:animate-none' />
              ) : null}
              {t('Start session')}
            </Button>
          ) : null}

          {session && isLive ? (
            <>
              <Button
                type='button'
                disabled={isBusy}
                onClick={() => {
                  runAction(() => stopMutation.mutate(session.session_id))
                }}
              >
                {stopMutation.isPending ? (
                  <Spinner className='size-4 motion-reduce:animate-none' />
                ) : null}
                {t('Stop session')}
              </Button>
              <Button
                type='button'
                variant='outline'
                disabled={isBusy}
                onClick={() => {
                  runAction(() => restartMutation.mutate(session.session_id))
                }}
              >
                {restartMutation.isPending ? (
                  <Spinner className='size-4 motion-reduce:animate-none' />
                ) : null}
                {t('Restart session')}
              </Button>
            </>
          ) : null}
        </div>

        {actionError || queryError ? (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>{t('Browser session unavailable')}</AlertTitle>
            <AlertDescription>
              {t(
                (actionError ?? queryError)?.messageKey ??
                  'Something went wrong. Please try again.'
              )}
            </AlertDescription>
            <Button type='button' size='sm' variant='outline' onClick={retry}>
              {t('Retry')}
            </Button>
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  )
}
