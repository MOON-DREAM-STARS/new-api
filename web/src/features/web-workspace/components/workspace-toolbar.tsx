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
import { Ellipsis, Maximize2, Minimize2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
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

type WorkspaceToolbarProps = {
  connection: WorkspaceConnection
  connectionDetail: string
  currentProjectName: string | null
  isLive: boolean
  isBusy: boolean
  immersive: boolean
  onStart: () => void
  onReconnect: () => void
  onRestart: () => void
  onStop: () => void
  onToggleImmersive: () => void
}

/**
 * Compact workspace toolbar. Low-frequency session operations live in the
 * overflow menu; the toolbar itself keeps the connection state, the current
 * project and the two reversible actions.
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
        {props.currentProjectName ? (
          <>
            <span aria-hidden='true'>·</span>
            <span className='text-foreground max-w-[12rem] truncate font-medium'>
              {props.currentProjectName}
            </span>
          </>
        ) : null}
      </span>

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
          <DropdownMenuLabel>{t('Browser session')}</DropdownMenuLabel>
          <DropdownMenuItem
            disabled={!props.isLive || props.isBusy}
            onSelect={props.onRestart}
          >
            {t('Restart session')}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!props.isLive || props.isBusy}
            onSelect={props.onStop}
          >
            {t('Stop session')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuLabel>{t('Connection details')}</DropdownMenuLabel>
          <p className='text-muted-foreground px-2 pb-1.5 text-xs'>
            {props.connectionDetail}
          </p>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
