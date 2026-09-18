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
import { Maximize2, Minimize2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'

import { useFullscreen } from '../hooks/use-fullscreen'
import { useRemoteSurface } from '../hooks/use-remote-surface'
import { isLiveSessionState } from '../lib/session'

type RemoteSurfaceProps = {
  sessionId: string | null
  sessionState: string | undefined
  /** False below the desktop breakpoint: the surface is declared unsupported there. */
  enabled: boolean
}

/**
 * Remote browser surface. Keyboard and mouse input go to the remote browser
 * through noVNC; the connect state, reconnect attempts and the manual reconnect
 * entry are always visible instead of failing silently.
 */
export function RemoteSurface(props: RemoteSurfaceProps) {
  const { t } = useTranslation()
  const isLive = isLiveSessionState(props.sessionState)
  const surfaceEnabled = props.enabled && isLive && Boolean(props.sessionId)
  const surface = useRemoteSurface({
    sessionId: props.sessionId ?? '',
    enabled: surfaceEnabled,
  })
  const fullscreen = useFullscreen(surface.containerRef)

  let statusText: string
  if (surface.status === 'connected') {
    statusText = t('Connected to the remote browser.')
  } else if (surface.status === 'reconnecting') {
    statusText = t('Reconnecting (attempt {{attempt}} of {{max}})...', {
      attempt: surface.attempt,
      max: surface.maxAttempts,
    })
  } else if (surface.status === 'failed') {
    statusText = t('Could not connect to the remote browser.')
  } else {
    statusText = t('Connecting to the remote browser...')
  }

  return (
    <Card className='flex h-full min-h-0 flex-col'>
      <CardHeader className='flex-row items-center justify-between gap-2'>
        <CardTitle>{t('Remote browser')}</CardTitle>
        {surfaceEnabled ? (
          <div className='flex gap-1'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => {
                if (fullscreen.isFullscreen) {
                  fullscreen.exitFullscreen()
                  return
                }
                fullscreen.enterFullscreen()
              }}
            >
              {fullscreen.isFullscreen ? (
                <Minimize2 aria-hidden='true' />
              ) : (
                <Maximize2 aria-hidden='true' />
              )}
              {fullscreen.isFullscreen
                ? t('Exit fullscreen')
                : t('Enter fullscreen')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={surface.reconnect}
            >
              <RefreshCw aria-hidden='true' />
              {t('Reconnect now')}
            </Button>
          </div>
        ) : null}
      </CardHeader>
      <CardContent className='flex min-h-0 flex-1 flex-col gap-2'>
        {surfaceEnabled ? (
          <p
            role='status'
            aria-live='polite'
            aria-atomic='true'
            className='text-muted-foreground flex items-center gap-2 text-sm'
          >
            {surface.status !== 'connected' ? (
              <span aria-hidden='true'>
                <Spinner className='size-4 motion-reduce:animate-none' />
              </span>
            ) : null}
            {statusText}
          </p>
        ) : null}

        {surfaceEnabled ? (
          <div
            ref={surface.containerRef}
            data-testid='web-workspace-surface'
            tabIndex={0}
            aria-label={t('Remote browser display')}
            className='relative aspect-video w-full overflow-hidden rounded-lg border bg-black'
          />
        ) : (
          <div className='text-muted-foreground flex aspect-video w-full items-center justify-center rounded-lg border border-dashed p-4 text-center text-sm'>
            {props.enabled
              ? t(
                  'The remote browser is unavailable while the session is not running.'
                )
              : t(
                  'The remote browser is disabled on small screens. Use a desktop viewport at least 1024px wide.'
                )}
          </div>
        )}

        {surfaceEnabled && surface.status === 'failed' ? (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>
              {t('Could not connect to the remote browser.')}
            </AlertTitle>
            <AlertDescription>
              {t(
                surface.errorMessageKey ??
                  'The browser agent is unavailable right now. Try again in a moment.'
              )}
            </AlertDescription>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={surface.reconnect}
            >
              {t('Reconnect now')}
            </Button>
          </Alert>
        ) : null}

        {fullscreen.isFullscreen ? (
          <p className='text-muted-foreground text-xs'>
            {t('Press Esc to exit fullscreen.')}
          </p>
        ) : null}

        {surfaceEnabled ? (
          <p className='text-muted-foreground text-xs'>
            {t('Keyboard and mouse input are sent to the remote browser.')}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}
