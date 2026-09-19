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
import { Plus, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  CONNECTION_DOT_CLASSES,
  CONNECTION_LABEL_KEYS,
  type WorkspaceConnection,
} from '../lib/connection'
import { projectAbbreviation } from '../lib/project-label'
import type { WebProject } from '../types'

type ProjectRailProps = {
  projects: WebProject[]
  selectedProjectId: number | null
  isLoading: boolean
  isError: boolean
  connection: WorkspaceConnection
  canCreate: boolean
  immersive: boolean
  onSelect: (project: WebProject) => void
  onCreate: () => void
  onOpenManager: () => void
}

function ConnectionIndicator(props: { connection: WorkspaceConnection }) {
  const { t } = useTranslation()
  const label = t(CONNECTION_LABEL_KEYS[props.connection])
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            role='status'
            aria-label={label}
            className={cn(
              'mt-1 size-2.5 shrink-0 rounded-full',
              CONNECTION_DOT_CLASSES[props.connection]
            )}
          />
        }
      />
      <TooltipContent side='right'>{label}</TooltipContent>
    </Tooltip>
  )
}

/**
 * Project rail: the only project navigation the workspace exposes. Every entry
 * is a real registered project of the signed-in user; there is no placeholder
 * project when the account has none.
 */
export function ProjectRail(props: ProjectRailProps) {
  const { t } = useTranslation()

  if (props.immersive) return null

  return (
    <TooltipProvider delay={200}>
      <aside
        aria-label={t('Projects')}
        className='bg-card/40 flex w-16 shrink-0 flex-col items-center gap-2 rounded-xl border py-2'
      >
        <ConnectionIndicator connection={props.connection} />

        <div className='flex min-h-0 flex-1 flex-col items-center gap-1.5 overflow-y-auto py-1'>
          {props.isLoading ? (
            <>
              <Skeleton className='size-10 rounded-lg' />
              <Skeleton className='size-10 rounded-lg' />
              <Skeleton className='size-10 rounded-lg' />
            </>
          ) : null}

          {!props.isLoading && props.isError ? (
            <span
              aria-hidden='true'
              className='text-muted-foreground text-xs'
              title={t('Could not load projects')}
            >
              !
            </span>
          ) : null}

          {!props.isLoading && !props.isError && props.projects.length === 0 ? (
            <span aria-hidden='true' className='text-muted-foreground text-xs'>
              –
            </span>
          ) : null}

          {props.projects.map((project) => {
            const selected = project.id === props.selectedProjectId
            return (
              <Tooltip key={project.id}>
                <TooltipTrigger
                  render={
                    <button
                      type='button'
                      aria-label={project.name}
                      aria-pressed={selected}
                      onClick={() => {
                        props.onSelect(project)
                      }}
                      className={cn(
                        'flex size-10 items-center justify-center rounded-lg border text-xs font-semibold transition-colors',
                        selected
                          ? 'border-primary/40 bg-primary/10 text-primary'
                          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground border-transparent'
                      )}
                    />
                  }
                >
                  {projectAbbreviation(project.name)}
                </TooltipTrigger>
                <TooltipContent side='right'>{project.name}</TooltipContent>
              </Tooltip>
            )
          })}
        </div>

        <div className='flex flex-col items-center gap-1.5'>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  aria-label={t('New project')}
                  disabled={!props.canCreate}
                  onClick={props.onCreate}
                />
              }
            >
              <Plus aria-hidden='true' />
            </TooltipTrigger>
            <TooltipContent side='right'>{t('New project')}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  aria-label={t('Project settings')}
                  onClick={props.onOpenManager}
                />
              }
            >
              <Settings aria-hidden='true' />
            </TooltipTrigger>
            <TooltipContent side='right'>
              {t('Project settings')}
            </TooltipContent>
          </Tooltip>
        </div>
      </aside>
    </TooltipProvider>
  )
}
