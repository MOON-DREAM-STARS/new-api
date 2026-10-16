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
import { ShieldAlert, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

/** A key created by the wizard; the full key is null until it is revealed. */
export type CreatedApiKey = {
  id: number
  name: string
  key: string | null
}

type RevealedApiKey = CreatedApiKey & { key: string }

function isRevealed(item: CreatedApiKey): item is RevealedApiKey {
  return item.key !== null
}

type KeyResultStepProps = {
  keys: CreatedApiKey[]
  /** How many keys were asked for; fewer were created when one failed. */
  requested: number
  revealing: boolean
  onRetryReveal: () => void
}

/** Last page of the wizard: the new keys, with a warning not to leak them. */
export function KeyResultStep(props: KeyResultStepProps) {
  const { t } = useTranslation()
  const revealed = props.keys.filter(isRevealed)
  const revealFailed = !props.revealing && revealed.length < props.keys.length
  return (
    <div className='space-y-4'>
      <Alert variant='destructive'>
        <ShieldAlert aria-hidden='true' />
        <AlertTitle className='font-semibold'>
          {t('Never share or leak this API Key!')}
        </AlertTitle>
        <AlertDescription>
          {t(
            'Anyone who has this key can use your quota. Copy it now and keep it safe. If you think it has leaked, disable or delete the key right away.'
          )}
        </AlertDescription>
      </Alert>
      {props.keys.length < props.requested && (
        <Alert>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {t(
              'Only {{created}} of {{requested}} API keys were created. Create the rest again to get them.',
              { created: props.keys.length, requested: props.requested }
            )}
          </AlertDescription>
        </Alert>
      )}
      <p className='text-sm font-medium'>
        {t('Successfully created {{count}} API Key(s)', {
          count: props.keys.length,
        })}
      </p>
      {props.revealing && (
        <div role='status' aria-label={t('Loading...')} className='space-y-2'>
          {props.keys.slice(0, 3).map((item) => (
            <Skeleton key={item.id} className='h-12 w-full' />
          ))}
        </div>
      )}
      {revealFailed && (
        <ErrorState
          className='min-h-[140px]'
          title={t('Failed to load the API key')}
          description={t(
            'The key was created. Retry, or copy it later from the API keys list.'
          )}
          onRetry={props.onRetryReveal}
        />
      )}
      <ul className='space-y-2'>
        {revealed.map((item) => (
          <li
            key={item.id}
            className='flex items-center gap-2 rounded-lg border p-2'
          >
            <div className='min-w-0 flex-1 space-y-1'>
              <p className='text-muted-foreground truncate text-xs'>
                {item.name}
              </p>
              <code className='bg-muted/50 block rounded-md px-2 py-1.5 font-mono text-xs break-all select-all'>
                {item.key}
              </code>
            </div>
            <CopyButton
              value={item.key}
              variant='outline'
              aria-label={t('Copy API key')}
            />
          </li>
        ))}
      </ul>
      {revealed.length > 1 && (
        <CopyButton
          value={revealed.map((item) => item.key).join('\n')}
          variant='outline'
          size='sm'
          aria-label={t('Copy all API keys')}
        >
          {t('Copy all')}
        </CopyButton>
      )}
    </div>
  )
}
