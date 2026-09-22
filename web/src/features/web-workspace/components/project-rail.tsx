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
import { Folder, Plus, Settings } from 'lucide-react'
import { useEffect, useRef } from 'react'
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

import type { WebProject } from '../types'

type ProjectRailProps = {
  projects: WebProject[]
  selectedProjectId: number | null
  isLoading: boolean
  isError: boolean
  canCreate: boolean
  onSelect: (project: WebProject) => void
  onCreate: () => void
  onOpenManager: () => void
}

/**
 * Horizontal project navigation for the workspace header. Tabs stay on one
 * line, scroll horizontally when needed, and keep their management controls
 * pinned at the end of the strip.
 */
export function ProjectRail(props: ProjectRailProps) {
  const { t } = useTranslation()
  const navRef = useRef<HTMLElement | null>(null)
  const selectedRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    const nav = navRef.current
    const selected = selectedRef.current
    if (!nav || !selected) return

    const navRect = nav.getBoundingClientRect()
    const selectedRect = selected.getBoundingClientRect()
    if (selectedRect.left < navRect.left) {
      nav.scrollLeft -= navRect.left - selectedRect.left
    } else if (selectedRect.right > navRect.right) {
      nav.scrollLeft += selectedRect.right - navRect.right
    }
  }, [props.projects, props.selectedProjectId])

  return (
    <TooltipProvider delay={200}>
      <div className='flex max-w-full min-w-0 items-center gap-1.5'>
        <nav
          ref={navRef}
          aria-label={t('Projects')}
          className='no-scrollbar min-w-0 flex-1 overflow-x-auto overscroll-x-contain'
        >
          <div className='flex w-max items-center gap-1'>
            {props.isLoading ? (
              <>
                <Skeleton className='h-9 w-32 shrink-0 rounded-lg' />
                <Skeleton className='h-9 w-32 shrink-0 rounded-lg' />
                <Skeleton className='h-9 w-32 shrink-0 rounded-lg' />
              </>
            ) : null}

            {!props.isLoading && props.isError ? (
              <span
                aria-hidden='true'
                className='text-muted-foreground shrink-0 text-xs'
                title={t('Could not load projects')}
              >
                !
              </span>
            ) : null}

            {!props.isLoading &&
            !props.isError &&
            props.projects.length === 0 ? (
              <span
                aria-hidden='true'
                className='text-muted-foreground shrink-0 text-xs'
              >
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
                        ref={selected ? selectedRef : undefined}
                        type='button'
                        aria-label={project.name}
                        aria-pressed={selected}
                        onClick={() => {
                          props.onSelect(project)
                        }}
                        className={cn(
                          'flex h-9 max-w-[14rem] shrink-0 items-center gap-2 rounded-lg border px-3 text-left text-sm font-medium transition-colors',
                          selected
                            ? 'bg-card text-foreground border-border shadow-sm'
                            : 'bg-muted/40 text-muted-foreground hover:bg-accent hover:text-accent-foreground border-transparent'
                        )}
                      />
                    }
                  >
                    <Folder aria-hidden='true' className='size-3.5 shrink-0' />
                    <span className='truncate'>{project.name}</span>
                  </TooltipTrigger>
                  <TooltipContent side='bottom'>{project.name}</TooltipContent>
                </Tooltip>
              )
            })}
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
              <TooltipContent side='bottom'>{t('New project')}</TooltipContent>
            </Tooltip>
          </div>
        </nav>

        <div className='flex shrink-0 items-center'>
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
            <TooltipContent side='bottom'>
              {t('Project settings')}
            </TooltipContent>
          </Tooltip>
        </div>
      </div>
    </TooltipProvider>
  )
}
