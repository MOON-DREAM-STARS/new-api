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
import { ExternalLinkIcon, RefreshCcwIcon } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { formatTimestamp, formatTimestampToDate } from '@/lib/format'

import { getDreamstarsReleaseUpdate } from '../api'
import { SettingsSection } from '../components/settings-section'
import type { DreamstarsReleaseUpdate } from '../types'

type UpdateCheckerSectionProps = {
  currentVersion?: string | null
  startTime?: number | null
}

function getReleaseStatusLabel(
  update: DreamstarsReleaseUpdate,
  t: (key: string) => string
) {
  if (update.source === 'stale') {
    return t(
      'Live release check is unavailable; showing previously verified release information.'
    )
  }

  switch (update.status) {
    case 'up_to_date':
      return t('You are running the latest Dreamstars release.')
    case 'update_available':
      return t('An approved Dreamstars fork release is available.')
    case 'current_release_unrecognized':
      return t(
        'Current release is not recognized as a Dreamstars fork release.'
      )
    default:
      return t('No approved Dreamstars fork release is currently available.')
  }
}

function getReleaseSourceLabel(
  source: DreamstarsReleaseUpdate['source'],
  t: (key: string) => string
) {
  switch (source) {
    case 'live':
      return t('Live data')
    case 'cached':
      return t('Cached data')
    case 'stale':
      return t('Stale cached data')
    default:
      return t('Unavailable')
  }
}

export function UpdateCheckerSection({
  currentVersion,
  startTime,
}: UpdateCheckerSectionProps) {
  const { t } = useTranslation()
  const [checking, setChecking] = useState(false)
  const [update, setUpdate] = useState<DreamstarsReleaseUpdate | null>(null)

  const uptime = startTime ? formatTimestamp(startTime) : t('Unknown')
  const version = update?.current_version || currentVersion || t('Unknown')

  const handleCheckUpdates = async () => {
    setChecking(true)
    try {
      const response = await getDreamstarsReleaseUpdate()
      if (!response.success) {
        throw new Error(response.message || t('Failed to check for updates'))
      }
      setUpdate(response.data)
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('Failed to check for updates')
      toast.error(message)
    } finally {
      setChecking(false)
    }
  }

  const openRelease = () => {
    if (update?.latest?.release_url) {
      window.open(update.latest.release_url, '_blank', 'noopener,noreferrer')
    }
  }

  return (
    <SettingsSection title={t('System maintenance')}>
      <div className='space-y-6'>
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='rounded-lg border p-4'>
            <div className='text-muted-foreground text-sm'>
              {t('Current version')}
            </div>
            <div className='text-lg font-semibold break-all'>{version}</div>
          </div>
          <div className='rounded-lg border p-4'>
            <div className='text-muted-foreground text-sm'>
              {t('Uptime since')}
            </div>
            <div className='text-lg font-semibold'>{uptime}</div>
          </div>
        </div>

        <div className='border-primary/30 bg-primary/5 rounded-lg border p-4 text-sm'>
          <p className='font-medium'>{t('Dreamstars fork updates')}</p>
          <p className='text-muted-foreground mt-1'>
            {t(
              'This page only checks Dreamstars fork releases. It never deploys or restarts the server.'
            )}
          </p>
        </div>

        <Button onClick={handleCheckUpdates} disabled={checking}>
          <RefreshCcwIcon
            className={`me-2 h-4 w-4 ${checking ? 'animate-spin' : ''}`}
          />
          {checking ? t('Checking updates...') : t('Check for updates')}
        </Button>

        {update && (
          <div
            className='space-y-4 rounded-lg border p-4'
            role='status'
            aria-live='polite'
          >
            <div className='space-y-1'>
              <p className='font-medium'>{getReleaseStatusLabel(update, t)}</p>
              {update.source === 'stale' && (
                <p className='text-sm text-amber-700 dark:text-amber-300'>
                  {t('Showing stale cached release information.')}
                </p>
              )}
            </div>

            {update.latest && (
              <dl className='m-0 grid gap-3 text-sm md:grid-cols-2'>
                <ReleaseField label={t('Latest approved release')}>
                  {update.latest.version}
                </ReleaseField>
                <ReleaseField label={t('Published')}>
                  {formatTimestampToDate(
                    new Date(update.latest.published_at).getTime(),
                    'milliseconds'
                  )}
                </ReleaseField>
                <ReleaseField label={t('Commit')}>
                  <code className='break-all'>{update.latest.commit}</code>
                </ReleaseField>
                <ReleaseField label={t('Image digest')}>
                  <code className='break-all'>{update.latest.digest}</code>
                </ReleaseField>
                <ReleaseField label={t('Release source')}>
                  {getReleaseSourceLabel(update.source, t)}
                </ReleaseField>
                <ReleaseField label={t('Container image')}>
                  <code className='break-all'>{update.latest.image}</code>
                </ReleaseField>
              </dl>
            )}

            {update.latest?.release_url && (
              <Button type='button' variant='secondary' onClick={openRelease}>
                <ExternalLinkIcon className='me-2 h-4 w-4' />
                {t('Open release')}
              </Button>
            )}
          </div>
        )}
      </div>
    </SettingsSection>
  )
}

function ReleaseField({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground'>{label}</dt>
      <dd className='mt-1 font-medium break-words'>{children}</dd>
    </div>
  )
}
