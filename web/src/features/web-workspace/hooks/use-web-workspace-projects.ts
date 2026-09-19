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

import {
  createWebWorkspaceProjectPermit,
  deleteWebWorkspaceProject,
  fetchWebWorkspaceProjects,
  renameWebWorkspaceProject,
} from '../api'
import { WEB_WORKSPACE_PROJECTS_QUERY_KEY } from '../constants'
import type { WebProject } from '../types'

/** Project list; the endpoint synchronises guard observations before listing. */
export function useWebWorkspaceProjects(enabled = true) {
  return useQuery({
    queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
    queryFn: fetchWebWorkspaceProjects,
    enabled,
    retry: false,
  })
}

/** Issues a creation permit. The permit alone never registers a project. */
export function useCreateWebWorkspaceProjectPermit() {
  return useMutation({
    mutationFn: (input: { name: string }) =>
      createWebWorkspaceProjectPermit(input.name),
  })
}

export type RenameProjectInput = {
  id: number
  name: string
}

/**
 * Renames a project with an optimistic list update; a failed request restores
 * the previous list so the UI never keeps an unconfirmed name.
 */
export function useRenameWebWorkspaceProject() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: RenameProjectInput) =>
      renameWebWorkspaceProject(input.id, input.name),
    onMutate: async (input) => {
      await queryClient.cancelQueries({
        queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
      })
      const previous = queryClient.getQueryData<WebProject[]>(
        WEB_WORKSPACE_PROJECTS_QUERY_KEY
      )
      queryClient.setQueryData<WebProject[]>(
        WEB_WORKSPACE_PROJECTS_QUERY_KEY,
        (current) =>
          (current ?? []).map((project) =>
            project.id === input.id ? { ...project, name: input.name } : project
          )
      )
      return { previous }
    },
    onError: (_error, _input, context) => {
      if (context?.previous) {
        queryClient.setQueryData(
          WEB_WORKSPACE_PROJECTS_QUERY_KEY,
          context.previous
        )
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
      })
    },
  })
}

export function useDeleteWebWorkspaceProject() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (projectId: number) => deleteWebWorkspaceProject(projectId),
    onSuccess: (_data, projectId) => {
      queryClient.setQueryData<WebProject[]>(
        WEB_WORKSPACE_PROJECTS_QUERY_KEY,
        (current) =>
          (current ?? []).filter((project) => project.id !== projectId)
      )
    },
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: WEB_WORKSPACE_PROJECTS_QUERY_KEY,
      })
    },
  })
}
