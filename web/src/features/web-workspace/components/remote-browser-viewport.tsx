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
import type { RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'

import type { RemoteSurfaceController } from '../hooks/use-remote-surface'
import type { LocalInputController } from '../hooks/use-local-input'
import { computePresentation } from '../lib/presentation'
import { isLiveSessionState } from '../lib/session'
import type { WebWorkspacePage } from '../types'

type RemoteBrowserViewportProps = {
  provider: string | null
  sessionId: string | null
  sessionState: string | undefined
  enabled: boolean
  immersive: boolean
  projectCreationFailed: boolean
  /** Pauses the visible stream while the real project navigation is pending. */
  suppressStream: boolean
  projectNavigationErrorKey?: string | null
  onRetryProject?: () => void
  /** Frame element and its measured box, owned by the page. */
  frameRef: RefObject<HTMLDivElement | null>
  frameSize: { width: number; height: number } | null
  /** True when the account really has no project: no placeholder is invented. */
  projectsEmpty: boolean
  surface: RemoteSurfaceController
  localInput: LocalInputController
  page: WebWorkspacePage | null
  startErrorMessageKey?: string | null
  isReloadPending: boolean
  onStart: () => void
  onReload: () => void
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

  const isLive = isLiveSessionState(props.sessionState)
  const surfaceEnabled =
    props.enabled && isLive && Boolean(props.sessionId ?? '')
  const presentation = computePresentation({
    provider: props.provider,
    screen: props.surface.screen,
    frame: props.frameSize,
  })
  const presentationReady = presentation.status === 'ready'
  const surfaceConnected =
    surfaceEnabled && props.surface.status === 'connected'
  // A guard page failure is the real, authoritative cause of an unopenable
  // project. It outranks the generic client-side navigation message so the
  // operator sees the actual state and error code instead of a guess.
  const pageFailure = Boolean(
    props.page && props.page.state !== 'READY' && props.page.error
  )
  // The overlay is driven by a client-side navigation failure. When the guard
  // also recorded a real page failure, that authoritative state and its error
  // code replace the classified client-side message, so a denied provider
  // document is never reported as a generic "could not open the project".
  const projectFailure = Boolean(props.projectNavigationErrorKey)
  // A failed project navigation must never hide or unmount the already-mounted
  // Kasm surface: the canvas keeps rendering and the error is layered on top.
  // Only the pre-navigation "adapting" state still covers the stream.
  const blockingStatus = props.suppressStream && !projectFailure
  const showStream = surfaceConnected && presentationReady && !blockingStatus

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
  } else if (!isLive && props.startErrorMessageKey) {
    status = {
      kind: 'failed',
      title: t('Could not start the remote browser.'),
      description: t(props.startErrorMessageKey),
      actionLabel: t('Retry'),
      onAction: props.onStart,
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
  } else if (blockingStatus && isLive) {
    status = {
      kind: 'adapting',
      title: t('Opening your project...'),
      description: t(
        'The workspace view stays paused until your project is ready.'
      ),
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
      {props.projectCreationFailed ? (
        <div
          role='alert'
          className='bg-destructive/5 text-destructive mb-3 rounded-lg border px-3 py-2 text-sm'
        >
          {t(
            'Please contact an administrator to manually enable project creation permission.'
          )}
        </div>
      ) : null}
      <div
        ref={props.frameRef}
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
                      width: props.frameSize?.width ?? 1280,
                      height: props.frameSize?.height ?? 720,
                    }
              }
              className='relative'
            />
          </div>
        </div>

        {props.localInput.enabled ? (
          <input
            ref={props.localInput.anchorRef}
            data-testid='web-workspace-local-input-anchor'
            aria-label={t('Local input anchor')}
            autoCapitalize='off'
            autoCorrect='off'
            spellCheck={false}
            tabIndex={-1}
            onCompositionStart={props.localInput.handleCompositionStart}
            onCompositionEnd={props.localInput.handleCompositionEnd}
            onInput={props.localInput.handleInput}
            onKeyDown={props.localInput.handleKeyDown}
            className='absolute h-px w-px border-0 bg-transparent p-0 opacity-0 outline-none'
            style={{ ...props.localInput.anchorStyle, pointerEvents: 'none' }}
          />
        ) : null}

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

        {projectFailure && isLive ? (
          <div
            role='alert'
            className='bg-background/95 absolute inset-x-0 top-0 z-30 flex items-center gap-3 border-b px-3 py-2 text-left shadow-sm backdrop-blur-sm'
          >
            <AlertCircle aria-hidden='true' className='size-4 shrink-0' />
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-medium'>
                {pageFailure
                  ? props.page?.state === 'RETRYING'
                    ? t('The remote page failed to load. Retrying...')
                    : t('The remote page could not open the selected project.')
                  : t('Could not open the selected project.')}
              </p>
              <p className='text-muted-foreground truncate font-mono text-xs'>
                {pageFailure
                  ? t('Error code: {{error}}', {
                      error: props.page?.error ?? '',
                    })
                  : t(props.projectNavigationErrorKey ?? '')}
              </p>
            </div>
            {props.onRetryProject ? (
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={props.isReloadPending}
                onClick={props.onRetryProject}
              >
                <RefreshCw aria-hidden='true' />
                {t('Retry')}
              </Button>
            ) : null}
          </div>
        ) : null}

        {props.page && props.page.state !== 'READY' && !projectFailure ? (
          <div
            role={props.page.state === 'FAILED' ? 'alert' : 'status'}
            aria-live='polite'
            className='bg-background/90 absolute bottom-3 left-3 z-20 flex max-w-[calc(100%-6rem)] items-center gap-3 rounded-lg border px-3 py-2 text-left shadow-sm backdrop-blur-sm'
          >
            <span
              aria-hidden='true'
              className='text-muted-foreground flex size-8 shrink-0 items-center justify-center'
            >
              {props.page.state === 'RETRYING' ? (
                <Spinner className='size-4 motion-reduce:animate-none' />
              ) : (
                <AlertCircle className='size-4' />
              )}
            </span>
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-medium'>
                {props.page.state === 'RETRYING'
                  ? t('The remote page failed to load. Retrying...')
                  : t('The remote page failed to load')}
              </p>
              <p className='text-muted-foreground truncate font-mono text-[11px]'>
                {props.page.state === 'RETRYING'
                  ? t('Retried {{attempts}} times · {{error}}', {
                      attempts: props.page.attempts,
                      error: props.page.error,
                    })
                  : t('Error code: {{error}}', { error: props.page.error })}
              </p>
            </div>
            {props.page.state === 'FAILED' ? (
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={props.isReloadPending}
                onClick={props.onReload}
              >
                <RefreshCw aria-hidden='true' />
                {t('Reload')}
              </Button>
            ) : null}
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
