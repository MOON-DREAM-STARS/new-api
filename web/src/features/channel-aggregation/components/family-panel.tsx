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
import { Layers, Pencil, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageFooterPortal } from '@/components/layout/components/page-footer'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { SERVICE_TIERS, tierLabel } from '@/features/service-tiers/lib/tiers'
import type { ServiceTierKey } from '@/features/service-tiers/types'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { deleteFamily, deletePolicy, getFamilyDetail, savePolicy } from '../api'
import {
  cloneConfig,
  completeConfig,
  policyProblems,
  sameConfig,
  type DraftProblem,
} from '../lib/policy'
import type {
  AggregationModel,
  AggregationPolicy,
  AggregationPolicyConfig,
  ModelFamily,
  PolicyInput,
} from '../types'
import { ChannelPoolSheet } from './channel-pool-sheet'
import { FamilyDialog } from './family-dialog'
import { MembersDialog } from './members-dialog'
import { RouteTestCard } from './route-test-card'
import { StrategyCard } from './strategy-card'
import { TierCard } from './tier-card'

type FamilyPanelProps = {
  family: ModelFamily
  families: ModelFamily[]
  models: AggregationModel[]
  groups: string[]
  onChanged: () => void
  onDeleted: () => void
}

type Draft = { enabled: boolean; config: AggregationPolicyConfig }

const FAMILY_SCOPE = '__family__'

export function FamilyPanel(props: FamilyPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const detailKey = ['channel-aggregation', 'family', props.family.id] as const
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: () => getFamilyDetail(props.family.id),
  })
  const [scope, setScope] = useState(FAMILY_SCOPE)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [poolTier, setPoolTier] = useState<ServiceTierKey | null>(null)
  const [membersOpen, setMembersOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [confirm, setConfirm] = useState<
    'toggle' | 'delete-override' | 'delete-family' | null
  >(null)

  const modelName = scope === FAMILY_SCOPE ? '' : scope
  const overrides = detail.data?.overrides ?? []
  const override = overrides.find((policy) => policy.model_name === modelName)
  const stored: AggregationPolicy | null | undefined = modelName
    ? override
    : detail.data?.default
  const inheriting = Boolean(modelName && !override)
  const base: Draft | null = useMemo(() => {
    if (!detail.data) return null
    const source = modelName && !override ? detail.data.default : stored
    return {
      enabled: source?.enabled ?? false,
      config: completeConfig(
        source?.config ?? detail.data.template,
        detail.data.template
      ),
    }
  }, [detail.data, modelName, override, stored])
  const current = draft ?? base
  // A new override is unsaved even while it still equals the default it copied.
  const dirty = Boolean(
    draft &&
    base &&
    (inheriting ||
      draft.enabled !== base.enabled ||
      !sameConfig(draft.config, base.config))
  )
  const editable = !inheriting || Boolean(draft)
  const problems: DraftProblem[] =
    current && editable ? policyProblems(current.config, current.enabled) : []
  const channels = useMemo(
    () =>
      new Map(
        (detail.data?.channels ?? []).map((channel) => [channel.id, channel])
      ),
    [detail.data]
  )

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: detailKey })
    props.onChanged()
  }
  const save = useMutation({
    mutationFn: (value: Draft) =>
      savePolicy({
        family_id: props.family.id,
        model_name: modelName,
        enabled: value.enabled,
        config: value.config,
        version: stored?.version ?? 0,
      }),
    onSuccess: (result) => {
      setDraft(null)
      setConfirm(null)
      toast.success(t('Aggregation rule saved'))
      for (const warning of result.warnings ?? []) {
        toast.warning(t(warning.key, warning.params))
      }
      refresh()
    },
    onError: (error) => {
      setConfirm(null)
      handleServerError(error, t('Failed to save'))
    },
  })
  const removeOverride = useMutation({
    mutationFn: () =>
      deletePolicy(props.family.id, modelName, override?.version ?? 0),
    onSuccess: () => {
      setDraft(null)
      setConfirm(null)
      toast.success(t('The model now inherits the family default'))
      refresh()
    },
    onError: (error) => handleServerError(error, t('Failed to delete')),
  })
  const removeFamily = useMutation({
    mutationFn: () => deleteFamily(props.family.id, props.family.version),
    onSuccess: () => {
      setConfirm(null)
      toast.success(t('Model family deleted'))
      props.onDeleted()
    },
    onError: (error) => handleServerError(error, t('Failed to delete')),
  })

  const memberOptions = [
    {
      value: FAMILY_SCOPE,
      label: t('All models (family default)'),
      description: t('{{count}} models', {
        count: props.family.members.length,
      }),
    },
    ...props.family.members.map((name) => ({
      value: name,
      label: name,
      description: overrides.some((policy) => policy.model_name === name)
        ? t('Override')
        : t('Inherits family default'),
    })),
  ]
  const update = (next: Draft) => setDraft(next)
  const pendingDraft: PolicyInput | null =
    current && editable && dirty
      ? {
          family_id: props.family.id,
          model_name: modelName,
          enabled: current.enabled,
          config: current.config,
          version: stored?.version ?? 0,
        }
      : null
  const submit = () => {
    if (!current) return
    if (base && current.enabled !== base.enabled && !inheriting) {
      setConfirm('toggle')
    } else save.mutate(current)
  }
  const problemText = (problem: DraftProblem) =>
    t(problem.key, {
      ...problem.params,
      tier: problem.params?.tier
        ? tierLabel(t, String(problem.params.tier))
        : undefined,
      previous: problem.params?.previous
        ? tierLabel(t, String(problem.params.previous))
        : undefined,
    })

  return (
    <div className='space-y-4 sm:space-y-5'>
      <Card>
        <CardContent className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
          <div className='min-w-0 space-y-1.5'>
            <div className='flex flex-wrap items-center gap-2'>
              <h3 className='text-base font-semibold'>{props.family.name}</h3>
              <Badge variant='outline' className='font-mono'>
                {props.family.key}
              </Badge>
              <Badge
                variant={props.family.default_enabled ? 'default' : 'secondary'}
              >
                {props.family.default_enabled
                  ? t('Aggregation on')
                  : t('Aggregation off')}
              </Badge>
            </div>
            <p className='text-muted-foreground text-xs'>
              {props.family.description ? `${props.family.description} · ` : ''}
              {t('{{count}} models', { count: props.family.members.length })}
              {' · '}
              {t('{{count}} overrides', { count: props.family.override_count })}
            </p>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={() => setMembersOpen(true)}
            >
              <Layers data-icon='inline-start' aria-hidden='true' />
              {t('Manage models')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={() => setEditOpen(true)}
            >
              <Pencil data-icon='inline-start' aria-hidden='true' />
              {t('Edit')}
            </Button>
            <Button
              type='button'
              variant='outline'
              className='text-destructive'
              onClick={() => setConfirm('delete-family')}
            >
              <Trash2 data-icon='inline-start' aria-hidden='true' />
              {t('Delete')}
            </Button>
          </div>
        </CardContent>
      </Card>

      {detail.isLoading && (
        <div
          role='status'
          aria-label={t('Loading...')}
          className='grid gap-3 md:grid-cols-3'
        >
          {SERVICE_TIERS.map((tier) => (
            <Skeleton key={tier} className='h-56 w-full' />
          ))}
        </div>
      )}
      {detail.error && (
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load the family')}</AlertTitle>
          <AlertDescription>
            {getServerErrorMessage(detail.error)}
          </AlertDescription>
        </Alert>
      )}

      {detail.data && current && (
        <>
          <div className='flex flex-col gap-3 rounded-xl border p-3 sm:p-4 lg:flex-row lg:items-end lg:justify-between'>
            <div className='min-w-0 space-y-1.5 lg:w-96'>
              <Label htmlFor='aggregation-scope'>{t('Aggregated model')}</Label>
              <Combobox
                id='aggregation-scope'
                options={memberOptions}
                value={scope}
                placeholder={t('Search models')}
                onValueChange={(value) => {
                  if (!value) return
                  setScope(value)
                  setDraft(null)
                }}
              />
            </div>
            <div className='flex flex-wrap items-center gap-3'>
              {inheriting ? (
                <>
                  <Badge variant='secondary'>
                    {t('Inherits family default')}
                  </Badge>
                  <Button
                    type='button'
                    variant='outline'
                    onClick={() =>
                      update({
                        enabled: base?.enabled ?? false,
                        config: cloneConfig(current.config),
                      })
                    }
                    disabled={Boolean(draft)}
                  >
                    {t('Create model override')}
                  </Button>
                </>
              ) : (
                <>
                  {modelName && (
                    <Badge variant='outline'>{t('Model override')}</Badge>
                  )}
                  <div className='flex items-center gap-2'>
                    <Switch
                      id='aggregation-enabled'
                      checked={current.enabled}
                      onCheckedChange={(enabled) =>
                        update({ ...current, enabled })
                      }
                    />
                    <Label htmlFor='aggregation-enabled'>
                      {modelName
                        ? t('Use aggregation for this model')
                        : t('Use aggregation for this family')}
                    </Label>
                  </div>
                  {override && (
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() => setConfirm('delete-override')}
                    >
                      {t('Remove override')}
                    </Button>
                  )}
                </>
              )}
            </div>
          </div>

          {inheriting && !draft && (
            <Alert>
              <AlertTitle>{t('This model uses the family default')}</AlertTitle>
              <AlertDescription>
                {t(
                  'The tiers below are the family default. Create an override to give this model its own tiers, ratios and channels.'
                )}
              </AlertDescription>
            </Alert>
          )}

          <section
            aria-label={t('Service tiers')}
            className='grid gap-3 md:grid-cols-3'
          >
            {SERVICE_TIERS.map((tier) => (
              <TierCard
                key={tier}
                tier={tier}
                config={current.config.tiers[tier]}
                channels={channels}
                disabled={inheriting && !draft}
                onChange={(tierConfig) =>
                  update({
                    ...current,
                    config: {
                      ...current.config,
                      tiers: { ...current.config.tiers, [tier]: tierConfig },
                    },
                  })
                }
                onEditPool={() => setPoolTier(tier)}
              />
            ))}
          </section>

          <StrategyCard
            strategy={current.config.strategy}
            groups={props.groups}
            disabled={inheriting && !draft}
            onChange={(strategy) =>
              update({ ...current, config: { ...current.config, strategy } })
            }
          />

          <RouteTestCard
            members={props.family.members}
            groups={props.groups}
            draft={pendingDraft}
          />

          {poolTier && (
            <ChannelPoolSheet
              open={Boolean(poolTier)}
              onOpenChange={(open) => !open && setPoolTier(null)}
              tier={poolTier}
              familyId={props.family.id}
              modelName={modelName}
              memberCount={props.family.members.length}
              entries={current.config.tiers[poolTier].channels}
              onApply={(entries) =>
                update({
                  ...current,
                  config: {
                    ...current.config,
                    tiers: {
                      ...current.config.tiers,
                      [poolTier]: {
                        ...current.config.tiers[poolTier],
                        channels: entries,
                      },
                    },
                  },
                })
              }
            />
          )}

          <PageFooterPortal>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='min-w-0 text-xs' aria-live='polite'>
                {problems.length > 0 ? (
                  <ul className='text-destructive space-y-0.5'>
                    {problems.slice(0, 3).map((problem) => (
                      <li
                        key={`${problem.key}-${String(problem.params?.tier ?? '')}`}
                      >
                        {problemText(problem)}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <span className='text-muted-foreground'>
                    {dirty
                      ? t('You have unsaved changes')
                      : t('All changes saved')}
                  </span>
                )}
              </div>
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
                  disabled={!dirty || problems.length > 0 || save.isPending}
                  onClick={submit}
                >
                  {save.isPending && <Spinner data-icon='inline-start' />}
                  {save.isPending ? t('Saving...') : t('Save')}
                </Button>
              </div>
            </div>
          </PageFooterPortal>

          <MembersDialog
            open={membersOpen}
            onOpenChange={setMembersOpen}
            family={props.family}
            families={props.families}
            models={props.models}
            overrideModels={overrides.map((policy) => policy.model_name)}
            onSaved={() => {
              setScope(FAMILY_SCOPE)
              setDraft(null)
              refresh()
            }}
          />
        </>
      )}

      <FamilyDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        family={props.family}
        nextSortOrder={props.family.sort_order}
        onSaved={refresh}
      />
      <ConfirmDialog
        open={confirm === 'toggle'}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={
          current?.enabled
            ? t('Turn on aggregation?')
            : t('Turn off aggregation?')
        }
        desc={
          current?.enabled
            ? t(
                'Requests for these models will be routed by service tier and billed at tier ratios instead of group ratios.'
              )
            : t(
                'Requests for these models will go back to ordinary group routing and group ratios.'
              )
        }
        confirmText={t('Save')}
        isLoading={save.isPending}
        handleConfirm={() => current && save.mutate(current)}
      />
      <ConfirmDialog
        open={confirm === 'delete-override'}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={t('Remove the override of {{model}}?', { model: modelName })}
        desc={t('The model will use the family default again.')}
        destructive
        confirmText={t('Remove override')}
        isLoading={removeOverride.isPending}
        handleConfirm={() => removeOverride.mutate()}
      />
      <ConfirmDialog
        open={confirm === 'delete-family'}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={t('Delete model family {{family}}?', {
          family: props.family.name,
        })}
        desc={t(
          'Its models, rules and overrides are deleted. Requests for these models go back to ordinary group routing.'
        )}
        destructive
        confirmText={t('Delete')}
        isLoading={removeFamily.isPending}
        handleConfirm={() => removeFamily.mutate()}
      />
    </div>
  )
}
