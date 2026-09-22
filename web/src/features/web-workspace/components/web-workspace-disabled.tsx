import { Link } from '@tanstack/react-router'
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
import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

import { useWebWorkspaceDenialReason } from '../hooks/use-web-workspace-denial-reason'
import { resolveDenialReasonKey } from '../lib/access'
import type { WebWorkspaceAccessState } from '../types'

type WebWorkspaceDisabledProps = {
  state: Exclude<WebWorkspaceAccessState, 'ready'>
}

/**
 * Explicit unavailable page. A disabled or unentitled account never gets a
 * silent 404: the reason is explained and a way back is offered. Hiding menu
 * entries is not the permission boundary — the server still enforces every
 * request.
 */
export function WebWorkspaceDisabled(props: WebWorkspaceDisabledProps) {
  const { t } = useTranslation()
  const reason = useWebWorkspaceDenialReason(true)
  const reasonKey = resolveDenialReasonKey(reason)

  const titleKey =
    props.state === 'disabled'
      ? 'Web Workspace is disabled'
      : 'Web Workspace is not available for this account'
  const descriptionKey =
    reasonKey ??
    (props.state === 'disabled'
      ? 'Web Workspace is disabled by the administrator.'
      : 'The feature is enabled but this account has no access. Contact an administrator if you believe you should have access.')

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full items-center justify-center'>
          <Empty className='max-w-lg border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <ShieldAlert aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t(titleKey)}</EmptyTitle>
              <EmptyDescription>{t(descriptionKey)}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Access is enforced by the server for every request, not by the navigation menu.'
                )}
              </p>
              <Button
                render={
                  <Link
                    to='/dashboard/$section'
                    params={{ section: 'overview' }}
                  />
                }
              >
                {t('Back to dashboard')}
              </Button>
            </EmptyContent>
          </Empty>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
