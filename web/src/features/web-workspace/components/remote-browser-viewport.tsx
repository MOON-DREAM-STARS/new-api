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
import { AlertCircle, Minimize2, RefreshCw } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'

import { useElementSize } from '../hooks/use-element-size'
import type { RemoteSurfaceController } from '../hooks/use-remote-surface'
import { computePresentation } from '../lib/presentation'
import { isLiveSessionState } from '../lib/session'

type RemoteBrowserViewportProps = {
  provider: string | null
  sessionId: string | null
  sessionState: string | undefined
  enabled: boolean
  immersive: boolean
  /** True when the account really has no project: no placeholder is invented. */
  projectsEmpty: boolean
  surface: RemoteSurfaceController
  onStart: () => void
  onExitImmersive: () => void
}

type ViewportStatusProps = {
  kind: 'empty' | 'starting' | 'connecting' | 'adapting' | 'failed' | 'mobile'
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
  children?: React.ReactNode
}

function ViewportStatus(props: ViewportStatusProps) {
  const isBusy = props.kind === 'starting' || props.kind === 'connecting'
  return (
    <div className='bg-background/90 absolute inset-0 flex items-center justify-center p-6'>
      <div className='flex max-w-md flex-col items-center gap-3 text-center'>
        <span
          aria-hidden='true'
          className='text-muted-foreground flex size-10 items-center justify-center rounded-full border'
        >
          {isBusy ? (
            <Spinner className='size-4 motion-reduce:animate-none' />
          ) : (
            <AlertCircle className='size-4' />
          )}
        </span>
        <p role='status' className='text-sm font-medium'>
          {props.title}
        </p>
        {props.description ? (
          <p className='text-muted-foreground text-xs'>{props.description}</p>
        ) : null}
        {props.children}
        {props.actionLabel && props.onAction ? (
          <Button type='button' size='sm' onClick={props.onAction}>
            {props.actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

/**
 * Workspace viewport: the remote browser is the primary surface.
 *
 * The renderer streams the real provider page; the presentation layer only
 * decides which part of that real framebuffer is shown. When the crop cannot
 * be derived from the real framebuffer and frame sizes the view fails closed:
 * the stream stays hidden behind an explanatory state instead of exposing the
 * provider shell.
 */
export function RemoteBrowserViewport(props: RemoteBrowserViewportProps) {
  const { t } = useTranslation()
  const frameRef = useRef<HTMLDivElement | null>(null)
  const frameSize = useElementSize(frameRef)

  const isLive = isLiveSessionState(props.sessionState)
  const surfaceEnabled =
    props.enabled && isLive && Boolean(props.sessionId ?? '')
  const presentation = computePresentation({
    provider: props.provider,
    screen: props.surface.screen,
    frame: frameSize,
  })
  const presentationReady = presentation.status === 'ready'
  const surfaceConnected =
    surfaceEnabled && props.surface.status === 'connected'
  const showStream = surfaceConnected && presentationReady

  let status: ViewportStatusProps | null = null
  if (!props.enabled) {
    status = {
      kind: 'mobile',
      title: t(
        'The remote browser is disabled on small screens. Use a desktop viewport at least 1024px wide.'
      ),
    }
  } else if (props.sessionState === 'STARTING') {
    status = {
      kind: 'starting',
      title: t('Starting the remote browser...'),
    }
  } else if (!isLive) {
    // A real account without projects gets its own empty state: projects are
    // created inside the remote browser, so the action stays "start session".
    status = props.projectsEmpty
      ? {
          kind: 'empty',
          title: t('No projects yet.'),
          description: t(
            'Create a project inside the remote browser and the system registers it here automatically.'
          ),
          actionLabel: t('Start session'),
          onAction: props.onStart,
        }
      : {
          kind: 'empty',
          title: t('Remote browser is not running'),
          description: t('Start a session to open the remote browser.'),
          actionLabel: t('Start session'),
          onAction: props.onStart,
        }
  } else if (props.surface.status === 'failed') {
    status = {
      kind: 'failed',
      title: t('Could not connect to the remote browser.'),
      description: t(
        props.surface.errorMessageKey ??
          'The browser agent is unavailable right now. Try again in a moment.'
      ),
      actionLabel: t('Reconnect now'),
      onAction: props.surface.reconnect,
    }
  } else if (!surfaceConnected) {
    status = {
      kind: 'connecting',
      title: t('Connecting to ChatGPT...'),
      description: t('Waiting for the remote page to become ready.'),
    }
  } else if (!presentationReady) {
    status = {
      kind: 'adapting',
      title: t('The remote page layout is being re-adapted.'),
      description: t(
        'The workspace view stays paused until the page layout can be mapped safely.'
      ),
    }
  }

  return (
    <div className='relative flex min-h-0 flex-1 flex-col'>
      <div
        ref={frameRef}
        data-testid='web-workspace-surface-frame'
        className={cn(
          'relative min-h-0 flex-1 overflow-hidden rounded-xl border bg-black',
          props.immersive ? 'rounded-none border-transparent' : null
        )}
      >
        <div
          aria-hidden={!showStream}
          className={cn('absolute inset-0', showStream ? null : 'invisible')}
        >
          <div
            className='absolute top-0 left-0'
            style={
              presentationReady
                ? {
                    transform: `translate3d(${presentation.transform.offsetX}px, ${presentation.transform.offsetY}px, 0)`,
                  }
                : undefined
            }
          >
            <div
              ref={props.surface.containerRef}
              data-testid='web-workspace-surface'
              tabIndex={showStream ? 0 : -1}
              aria-label={t('Remote browser display')}
              style={
                presentationReady
                  ? {
                      width: presentation.transform.stageWidth,
                      height: presentation.transform.stageHeight,
                    }
                  : {
                      width: frameSize?.width ?? 1280,
                      height: frameSize?.height ?? 720,
                    }
              }
              className='relative'
            />
          </div>
        </div>

        {status ? <ViewportStatus {...status} /> : null}

        {surfaceEnabled && props.surface.status === 'reconnecting' ? (
          <div className='bg-background/85 absolute inset-x-0 top-0 flex items-center justify-center gap-2 px-3 py-2 text-xs'>
            <Spinner className='size-3.5 motion-reduce:animate-none' />
            {t(
              'Connection lost. Reconnecting (attempt {{attempt}} of {{max}})...',
              {
                attempt: props.surface.attempt,
                max: props.surface.maxAttempts,
              }
            )}
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={props.surface.reconnect}
            >
              <RefreshCw aria-hidden='true' />
              {t('Reconnect now')}
            </Button>
          </div>
        ) : null}

        {props.immersive ? (
          <Button
            type='button'
            size='sm'
            variant='secondary'
            className='absolute right-3 bottom-3 opacity-70 hover:opacity-100'
            onClick={props.onExitImmersive}
          >
            <Minimize2 aria-hidden='true' />
            {t('Exit immersive mode')}
          </Button>
        ) : null}
      </div>
    </div>
  )
}
