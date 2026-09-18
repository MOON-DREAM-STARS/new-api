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
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

import { ProjectPanel } from './components/project-panel'
import { RemoteSurface } from './components/remote-surface'
import { SessionPanel } from './components/session-panel'
import { WebWorkspaceDisabled } from './components/web-workspace-disabled'
import { useDesktopViewport } from './hooks/use-desktop-viewport'
import { useWebWorkspaceConfig } from './hooks/use-web-workspace-config'
import { useWebWorkspaceSession } from './hooks/use-web-workspace-session'
import { resolveWebWorkspaceAccess } from './lib/access'
import { classifyWebWorkspaceError } from './lib/errors'

/**
 * Web Workspace page. The route guard already primed the capability probe, so
 * a disabled or unentitled account renders an explained state page instead of
 * a silent 404.
 */
export function WebWorkspace() {
  const { t } = useTranslation()
  const configQuery = useWebWorkspaceConfig()
  const isDesktop = useDesktopViewport()
  const access = configQuery.data
    ? resolveWebWorkspaceAccess(configQuery.data)
    : null
  const sessionQuery = useWebWorkspaceSession({ enabled: access === 'ready' })
  const session = sessionQuery.data ?? null

  if (configQuery.isPending) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p className='text-muted-foreground flex items-center gap-2 py-8 text-sm'>
            <Spinner className='size-4 motion-reduce:animate-none' />
            {t('Loading Web Workspace...')}
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (configQuery.isError || !configQuery.data) {
    const error = configQuery.isError
      ? classifyWebWorkspaceError(configQuery.error)
      : null
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <Alert variant='destructive' role='alert' className='mt-4 max-w-xl'>
            <AlertTitle>{t('Could not load Web Workspace')}</AlertTitle>
            <AlertDescription>
              {t(
                error?.messageKey ?? 'Something went wrong. Please try again.'
              )}
            </AlertDescription>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => {
                void configQuery.refetch()
              }}
            >
              {t('Retry')}
            </Button>
          </Alert>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (access !== 'ready') {
    return <WebWorkspaceDisabled state={access ?? 'not-entitled'} />
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='grid h-full min-h-0 gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]'>
          <div className='flex min-h-0 flex-col gap-4 overflow-auto pr-1'>
            {isDesktop ? null : (
              <Alert variant='destructive' role='alert'>
                <AlertTitle>{t('Desktop required')}</AlertTitle>
                <AlertDescription>
                  {t(
                    'Web Workspace is not supported on mobile. Use a desktop viewport at least 1024px wide.'
                  )}
                </AlertDescription>
              </Alert>
            )}
            <SessionPanel />
            <ProjectPanel />
          </div>
          <div className='min-h-0'>
            <RemoteSurface
              sessionId={session?.session_id ?? null}
              sessionState={session?.state}
              enabled={isDesktop}
            />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
