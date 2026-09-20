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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ProjectDeleteDialog } from '../components/project-delete-dialog'

const originalDelete = api.delete

afterEach(() => {
  api.delete = originalDelete
})

test('blocks deleting the last project and offers creation instead', async () => {
  const deleteRequest = vi.fn()
  api.delete = deleteRequest
  const onCreate = vi.fn()
  const onOpenChange = vi.fn()
  const onDeleted = vi.fn()
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <ProjectDeleteDialog
        project={{
          id: 1,
          provider: 'chatgpt',
          name: 'User-Project',
          created_at: 1,
          updated_at: 1,
        }}
        isLastProject
        onCreate={onCreate}
        onOpenChange={onOpenChange}
        onDeleted={onDeleted}
      />
    </QueryClientProvider>
  )

  expect(screen.getByText('Cannot delete the last project')).toBeInTheDocument()
  expect(
    screen.getByText(
      'At least one project must remain. Create another project before deleting this one.'
    )
  ).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'New project' }))

  expect(deleteRequest).not.toHaveBeenCalled()
  expect(onDeleted).not.toHaveBeenCalled()
  expect(onOpenChange).toHaveBeenCalledWith(false)
  expect(onCreate).toHaveBeenCalledTimes(1)
})

test('shows last-project guidance when the server rejects the delete', async () => {
  api.delete = async () => {
    throw {
      response: {
        status: 409,
        data: {
          success: false,
          code: 'WEB_WORKSPACE_LAST_PROJECT_REQUIRED',
        },
      },
    }
  }
  const onCreate = vi.fn()
  const onOpenChange = vi.fn()
  const onDeleted = vi.fn()
  const onDeleteRejected = vi.fn()
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <ProjectDeleteDialog
        project={{
          id: 1,
          provider: 'chatgpt',
          name: 'User-Project',
          created_at: 1,
          updated_at: 1,
        }}
        isLastProject={false}
        onCreate={onCreate}
        onOpenChange={onOpenChange}
        onDeleted={onDeleted}
        onDeleteRejected={onDeleteRejected}
      />
    </QueryClientProvider>
  )

  await userEvent.click(screen.getByRole('button', { name: 'Delete project' }))

  expect(
    await screen.findByText('Cannot delete the last project')
  ).toBeInTheDocument()
  expect(
    screen.getByText(
      'At least one project must remain. Create another project before deleting this one.'
    )
  ).toBeInTheDocument()
  expect(onDeleteRejected).toHaveBeenCalledWith(1)
  await userEvent.click(screen.getByRole('button', { name: 'New project' }))
  expect(onCreate).toHaveBeenCalledTimes(1)
})
