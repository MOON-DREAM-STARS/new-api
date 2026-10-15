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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getAggregationOverview } from './api'
import { FamilyDialog } from './components/family-dialog'
import { FamilyPanel } from './components/family-panel'

const overviewKey = ['channel-aggregation', 'overview'] as const

export function ChannelAggregation() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const overview = useQuery({
    queryKey: overviewKey,
    queryFn: getAggregationOverview,
  })
  const [selected, setSelected] = useState<number | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const families = overview.data?.families ?? []
  const family = families.find((item) => item.id === selected) ?? families[0]
  const refresh = () =>
    void queryClient.invalidateQueries({ queryKey: ['channel-aggregation'] })

  let content
  if (overview.isLoading) {
    content = (
      <div role='status' aria-label={t('Loading...')} className='space-y-4'>
        <Skeleton className='h-9 w-80 max-w-full' />
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    )
  } else if (!overview.data) {
    content = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('Failed to load channel aggregation')}</EmptyTitle>
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
  } else if (!family) {
    content = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('No model family yet')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Create a family such as DeepSeek or Qwen, add its models, then give each tier a pool of existing channels.'
            )}
          </EmptyDescription>
        </EmptyHeader>
        <Button type='button' onClick={() => setCreateOpen(true)}>
          <Plus data-icon='inline-start' aria-hidden='true' />
          {t('New model family')}
        </Button>
      </Empty>
    )
  } else {
    content = (
      <div className='space-y-4'>
        <Tabs
          value={family.id}
          onValueChange={(value) => setSelected(Number(value))}
        >
          <div className='-mx-1 overflow-x-auto px-1 pb-1'>
            <TabsList aria-label={t('Model families')}>
              {families.map((item) => (
                <TabsTrigger key={item.id} value={item.id} className='gap-1.5'>
                  {item.name}
                  <Badge
                    variant={item.default_enabled ? 'default' : 'secondary'}
                    className='h-4 px-1.5 text-[10px]'
                  >
                    {item.members.length}
                  </Badge>
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
        </Tabs>
        <FamilyPanel
          key={family.id}
          family={family}
          families={families}
          models={overview.data.models}
          groups={overview.data.groups}
          onChanged={refresh}
          onDeleted={() => {
            setSelected(null)
            refresh()
          }}
        />
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Channel Aggregation')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          size='sm'
          onClick={() => setCreateOpen(true)}
          disabled={!overview.data}
        >
          <Plus data-icon='inline-start' aria-hidden='true' />
          {t('New model family')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>{content}</div>
        <FamilyDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          nextSortOrder={(families.at(-1)?.sort_order ?? 0) + 1}
          onSaved={(id) => {
            setSelected(id)
            refresh()
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
