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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Info, Route as RouteIcon, ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PageFooterPortal } from '@/components/layout/components/page-footer'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { useDebounce } from '@/hooks/use-debounce'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getServiceTierOverview,
  previewServiceTierSettings,
  saveServiceTierSettings,
  serviceTierOverviewQueryKey,
} from './api'
import { ServiceTierSettingsEditor } from './components/service-tier-settings-editor'
import { StrategyPreview } from './components/strategy-preview'
import {
  draftProblem,
  sameSettings,
  SERVICE_TIERS,
  tierRatioRanges,
} from './lib/tiers'
import type { ServiceTierSettings } from './types'

export function ServiceTiers() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const overview = useQuery({
    queryKey: serviceTierOverviewQueryKey,
    queryFn: getServiceTierOverview,
  })
  const [draft, setDraft] = useState<ServiceTierSettings | null>(null)
  const saved = overview.data?.settings
  const current = draft ?? saved ?? null
  const dirty = Boolean(current && saved && !sameSettings(current, saved))
  const problem = current ? draftProblem(current) : null
  const ratios = useMemo(() => tierRatioRanges(overview.data), [overview.data])

  const previewInput = useDebounce(current, 200)
  const preview = useQuery({
    queryKey: ['service-tier', 'preview', previewInput],
    queryFn: ({ signal }) =>
      previewServiceTierSettings(previewInput as ServiceTierSettings, signal),
    enabled: Boolean(previewInput && dirty && !problem),
    placeholderData: (previous) => previous,
    meta: { errorToast: false },
  })

  const save = useMutation({
    mutationFn: (settings: ServiceTierSettings) =>
      saveServiceTierSettings(settings),
    onSuccess: (data) => {
      queryClient.setQueryData(serviceTierOverviewQueryKey, data)
      setDraft(null)
      toast.success(t('Service tier settings saved'))
    },
    onError: (error) => handleServerError(error, t('Failed to save')),
  })

  const update = (next: ServiceTierSettings) => setDraft(next)

  let content
  if (overview.isLoading) {
    content = (
      <div role='status' aria-label={t('Loading...')} className='space-y-4'>
        <div className='grid gap-3 sm:grid-cols-3'>
          {SERVICE_TIERS.map((tier) => (
            <Skeleton key={tier} className='h-36 w-full' />
          ))}
        </div>
        <Skeleton className='h-28 w-full' />
        <Skeleton className='h-40 w-full' />
      </div>
    )
  } else if (!overview.data || !current) {
    content = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('Failed to load service tiers')}</EmptyTitle>
          <EmptyDescription>
            {overview.error
              ? getServerErrorMessage(overview.error)
              : t('Refresh the list and try again.')}
          </EmptyDescription>
        </EmptyHeader>
        <Button
          type='button'
          variant='outline'
          onClick={() => void overview.refetch()}
        >
          {t('Retry')}
        </Button>
      </Empty>
    )
  } else {
    const previewData = dirty ? preview.data : overview.data.preview
    content = (
      <div className='grid gap-4 sm:gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(340px,0.42fr)] xl:items-start'>
        <div className='min-w-0 space-y-4 sm:space-y-5'>
          {!overview.data.available && (
            <Alert>
              <Info className='size-4' aria-hidden='true' />
              <AlertTitle>{t('No model uses service tiers yet')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Your choices are saved and apply once the administrator turns on service tiers for a model family.'
                )}
              </AlertDescription>
            </Alert>
          )}
          <ServiceTierSettingsEditor
            settings={current}
            ratios={ratios}
            scope='account'
            disabled={save.isPending}
            onChange={update}
          />
        </div>
        <aside className='min-w-0 space-y-4 xl:sticky xl:top-0'>
          <Card>
            <CardHeader>
              <CardTitle className='flex items-center gap-2 text-sm'>
                <RouteIcon className='size-4' aria-hidden='true' />
                {t('Strategy preview')}
              </CardTitle>
              <CardDescription>
                {dirty
                  ? t('Preview of unsaved changes')
                  : t('The strategy your requests use now')}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <StrategyPreview
                preview={previewData}
                loading={
                  dirty && (preview.isFetching || previewInput !== current)
                }
                problem={
                  problem ??
                  (preview.error ? getServerErrorMessage(preview.error) : null)
                }
              />
            </CardContent>
          </Card>
          <p className='text-muted-foreground flex items-start gap-2 px-1 text-xs leading-relaxed'>
            <ShieldCheck
              className='mt-0.5 size-3.5 shrink-0'
              aria-hidden='true'
            />
            {t(
              'Each request is charged once, at the ratio of the tier that served it. Failed attempts are not charged, and a tier your balance cannot cover is never tried.'
            )}
          </p>
        </aside>
        <PageFooterPortal>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-muted-foreground text-xs' aria-live='polite'>
              {dirty && t('You have unsaved changes')}
              {!dirty &&
                current.is_default &&
                t('Using the default: lowest price first, no escalation')}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                disabled={!dirty || save.isPending}
                onClick={() => setDraft(null)}
              >
                {t('Reset')}
              </Button>
              <Button
                type='button'
                disabled={!dirty || Boolean(problem) || save.isPending}
                onClick={() => save.mutate(current)}
              >
                {save.isPending && <Spinner data-icon='inline-start' />}
                {save.isPending ? t('Saving...') : t('Save')}
              </Button>
            </div>
          </div>
        </PageFooterPortal>
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Service Tiers')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>{content}</div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
