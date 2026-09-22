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
import {
  ArrowLeft,
  ArrowRight,
  ClipboardCopy,
  ClipboardPaste,
  Ellipsis,
  Keyboard,
  Maximize2,
  Minimize2,
  RefreshCw,
  RotateCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

import {
  CONNECTION_DOT_CLASSES,
  CONNECTION_LABEL_KEYS,
  type WorkspaceConnection,
} from '../lib/connection'
import type { WebWorkspaceNavigation } from '../types'

export type WorkspaceNavigationControlsProps = {
  navigation: WebWorkspaceNavigation | null
  isAvailable: boolean
  isPending: boolean
  onBack: () => void
  onForward: () => void
  onReload: () => void
}

/** Shared real-navigation controls for the toolbar and immersive overlay. */
export function WorkspaceNavigationControls(
  props: WorkspaceNavigationControlsProps
) {
  const { t } = useTranslation()
  const isBackDisabled =
    props.isPending ||
    !props.isAvailable ||
    props.navigation?.can_go_back !== true
  const isForwardDisabled =
    props.isPending ||
    !props.isAvailable ||
    props.navigation?.can_go_forward !== true
  const isReloadDisabled = props.isPending || !props.isAvailable

  return (
    <div className='flex items-center gap-2'>
      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Browser back')}
        disabled={isBackDisabled}
        onClick={props.onBack}
      >
        <ArrowLeft aria-hidden='true' />
      </Button>
      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Browser forward')}
        disabled={isForwardDisabled}
        onClick={props.onForward}
      >
        <ArrowRight aria-hidden='true' />
      </Button>
      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Refresh page')}
        disabled={isReloadDisabled}
        onClick={props.onReload}
      >
        <RotateCw aria-hidden='true' />
      </Button>
    </div>
  )
}

type WorkspaceToolbarProps = {
  connection: WorkspaceConnection
  connectionDetail: string
  sessionMode: string | null
  isLive: boolean
  isBusy: boolean
  navigation: WebWorkspaceNavigation | null
  isNavigationAvailable: boolean
  isNavigationPending: boolean
  inputMode: 'remote' | 'local'
  inputEnabled: boolean
  immersive: boolean
  onStart: () => void
  onReconnect: () => void
  onRestart: () => void
  onLockSession: () => void
  onStop: () => void
  onNavigateBack: () => void
  onNavigateForward: () => void
  onNavigateReload: () => void
  onCopyRemoteSelection: () => void
  onPasteIntoRemote: () => void
  onToggleImmersive: () => void
  onToggleInputMode: () => void
}

/**
 * Compact workspace toolbar. Low-frequency session operations live in the
 * overflow menu; the toolbar itself keeps the connection state and the two
 * reversible navigation actions.
 */
export function WorkspaceToolbar(props: WorkspaceToolbarProps) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-wrap items-center justify-end gap-1.5 sm:gap-2'>
      <span
        role='status'
        aria-live='polite'
        className='text-muted-foreground flex items-center gap-1.5 text-xs sm:text-sm'
      >
        <span
          aria-hidden='true'
          className={cn(
            'size-2 shrink-0 rounded-full',
            CONNECTION_DOT_CLASSES[props.connection]
          )}
        />
        {t(CONNECTION_LABEL_KEYS[props.connection])}
      </span>

      {props.sessionMode === 'LOGIN' ? (
        <span className='rounded-full border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-600 dark:text-amber-400'>
          {t('Login mode')}
        </span>
      ) : null}

      {props.isLive ? null : (
        <Button
          type='button'
          size='sm'
          disabled={props.isBusy}
          onClick={props.onStart}
        >
          {t('Start session')}
        </Button>
      )}

      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Reconnect now')}
        disabled={!props.isLive}
        onClick={props.onReconnect}
      >
        <RefreshCw aria-hidden='true' />
      </Button>

      {props.immersive ? null : (
        <WorkspaceNavigationControls
          navigation={props.navigation}
          isAvailable={props.isNavigationAvailable}
          isPending={props.isNavigationPending}
          onBack={props.onNavigateBack}
          onForward={props.onNavigateForward}
          onReload={props.onNavigateReload}
        />
      )}

      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Copy remote selection')}
        title={t('Copy remote selection')}
        disabled={!props.isLive || props.isBusy}
        onClick={props.onCopyRemoteSelection}
      >
        <ClipboardCopy aria-hidden='true' />
      </Button>
      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={t('Paste into remote')}
        title={t('Paste into remote')}
        disabled={!props.isLive || props.isBusy}
        onClick={props.onPasteIntoRemote}
      >
        <ClipboardPaste aria-hidden='true' />
      </Button>

      <Button
        type='button'
        size='icon-sm'
        variant='ghost'
        aria-label={
          props.immersive ? t('Exit immersive mode') : t('Immersive mode')
        }
        aria-pressed={props.immersive}
        onClick={props.onToggleImmersive}
      >
        {props.immersive ? (
          <Minimize2 aria-hidden='true' />
        ) : (
          <Maximize2 aria-hidden='true' />
        )}
      </Button>

      <Button
        type='button'
        size='sm'
        variant={props.inputMode === 'local' ? 'secondary' : 'ghost'}
        aria-pressed={props.inputMode === 'local'}
        aria-label={
          props.inputMode === 'local' ? t('Local input') : t('Remote keyboard')
        }
        title={
          props.inputMode === 'local'
            ? t('Switch to remote keyboard')
            : t('Switch to local input')
        }
        disabled={!props.inputEnabled || props.isBusy}
        onClick={props.onToggleInputMode}
      >
        <Keyboard aria-hidden='true' />
        {props.inputMode === 'local' ? t('Local input') : t('Remote keyboard')}
      </Button>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type='button'
              size='icon-sm'
              variant='ghost'
              aria-label={t('More workspace actions')}
            />
          }
        >
          <Ellipsis aria-hidden='true' />
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-56'>
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t('Browser session')}</DropdownMenuLabel>
            <DropdownMenuItem
              disabled={!props.isLive || props.isBusy}
              onSelect={props.onRestart}
            >
              {t('Restart session')}
            </DropdownMenuItem>
            {props.sessionMode === 'LOGIN' ? (
              <DropdownMenuItem
                disabled={!props.isLive || props.isBusy}
                onSelect={props.onLockSession}
              >
                {t('Finish sign-in and lock')}
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem
              disabled={!props.isLive || props.isBusy}
              onSelect={props.onStop}
            >
              {t('Stop session')}
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t('Connection details')}</DropdownMenuLabel>
            <p className='text-muted-foreground px-2 pb-1.5 text-xs'>
              {props.connectionDetail}
            </p>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
