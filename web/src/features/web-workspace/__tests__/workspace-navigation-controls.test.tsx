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
import { describe, expect, test, vi } from 'vitest'

import { WorkspaceNavigationControls } from '../components/workspace-toolbar'
import type { WebWorkspaceNavigation } from '../types'

const availableNavigation: WebWorkspaceNavigation = {
  can_go_back: true,
  can_go_forward: true,
  updated_at: 1,
}

function renderControls(input: {
  navigation: WebWorkspaceNavigation | null
  isAvailable?: boolean
  isPending?: boolean
}) {
  const onBack = vi.fn()
  const onForward = vi.fn()
  const onReload = vi.fn()
  render(
    <WorkspaceNavigationControls
      navigation={input.navigation}
      isAvailable={input.isAvailable ?? true}
      isPending={input.isPending ?? false}
      onBack={onBack}
      onForward={onForward}
      onReload={onReload}
    />
  )
  return { onBack, onForward, onReload }
}

function button(name: string): HTMLButtonElement {
  return screen.getByRole('button', { name }) as HTMLButtonElement
}

describe('WorkspaceNavigationControls', () => {
  test('renders accessible controls and fails closed without navigation state', () => {
    renderControls({ navigation: null })

    expect(button('Browser back').disabled).toBe(true)
    expect(button('Browser forward').disabled).toBe(true)
    expect(button('Refresh page').disabled).toBe(false)
  })

  test('uses the real history flags for each direction', () => {
    renderControls({
      navigation: {
        ...availableNavigation,
        can_go_back: false,
        can_go_forward: false,
      },
    })

    expect(button('Browser back').disabled).toBe(true)
    expect(button('Browser forward').disabled).toBe(true)
    expect(button('Refresh page').disabled).toBe(false)
  })

  test('calls the real command handlers when navigation is available', () => {
    const handlers = renderControls({ navigation: availableNavigation })

    button('Browser back').click()
    button('Browser forward').click()
    button('Refresh page').click()

    expect(handlers.onBack).toHaveBeenCalledOnce()
    expect(handlers.onForward).toHaveBeenCalledOnce()
    expect(handlers.onReload).toHaveBeenCalledOnce()
  })

  test('disables every control while a navigation command is pending', () => {
    renderControls({ navigation: availableNavigation, isPending: true })

    expect(button('Browser back').disabled).toBe(true)
    expect(button('Browser forward').disabled).toBe(true)
    expect(button('Refresh page').disabled).toBe(true)
  })

  test('disables every control when the live surface is unavailable', () => {
    renderControls({ navigation: availableNavigation, isAvailable: false })

    expect(button('Browser back').disabled).toBe(true)
    expect(button('Browser forward').disabled).toBe(true)
    expect(button('Refresh page').disabled).toBe(true)
  })
})
