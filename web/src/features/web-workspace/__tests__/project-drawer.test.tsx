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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { ProjectDrawer } from '../components/project-drawer'
import type { WebProject } from '../types'

const projects: WebProject[] = [
  {
    id: 1,
    provider: 'chatgpt',
    name: 'Alpha',
    created_at: 1,
    updated_at: 1,
  },
  {
    id: 2,
    provider: 'chatgpt',
    name: 'Beta',
    created_at: 2,
    updated_at: 2,
  },
]

function renderDrawer(overrides: Partial<React.ComponentProps<typeof ProjectDrawer>> = {}) {
  const props: React.ComponentProps<typeof ProjectDrawer> = {
    open: true,
    onOpenChange: vi.fn(),
    projects,
    isLoading: false,
    errorMessageKey: null,
    selectedProjectId: 1,
    canCreate: true,
    removalNames: [],
    onDismissRemoval: vi.fn(),
    onRefetch: vi.fn(),
    onSelect: vi.fn(),
    onCreate: vi.fn(),
    onRename: vi.fn(),
    onDelete: vi.fn(),
    ...overrides,
  }
  return { ...render(<ProjectDrawer {...props} />), props }
}

describe('ProjectDrawer', () => {
  test('renders as a centred dialog with the complete project list', () => {
    renderDrawer()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Alpha' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('button', { name: 'Beta' })).toHaveAttribute(
      'aria-pressed',
      'false'
    )
    expect(screen.getByRole('button', { name: 'New project' })).toBeEnabled()
  })

  test('forwards selection, create, and refresh actions', async () => {
    const { props } = renderDrawer()

    await userEvent.click(screen.getByRole('button', { name: 'Beta' }))
    await userEvent.click(screen.getByRole('button', { name: 'New project' }))
    await userEvent.click(
      screen.getByRole('button', { name: 'Refresh projects' })
    )

    expect(props.onSelect).toHaveBeenCalledWith(projects[1])
    expect(props.onCreate).toHaveBeenCalledTimes(1)
    expect(props.onRefetch).toHaveBeenCalledTimes(1)
  })

  test('forwards rename and delete actions for the selected row', async () => {
    const { props } = renderDrawer()

    await userEvent.click(
      screen.getByRole('button', { name: 'Rename Alpha' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Delete Alpha' })
    )

    expect(props.onRename).toHaveBeenCalledWith(projects[0])
    expect(props.onDelete).toHaveBeenCalledWith(projects[0])
  })

  test('disables creation when the live-session permission is absent', () => {
    renderDrawer({ canCreate: false })

    expect(screen.getByRole('button', { name: 'New project' })).toBeDisabled()
  })

  test('renders the real empty state without inventing a project', () => {
    renderDrawer({ projects: [], selectedProjectId: null })

    expect(
      screen.getByText(
        'No projects yet. Create one inside the remote browser and it is registered here automatically.'
      )
    ).toBeInTheDocument()
  })

  test('renders the loading state', () => {
    renderDrawer({ isLoading: true })

    expect(screen.getByText('Loading projects...')).toBeInTheDocument()
  })
})
