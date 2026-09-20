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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import {
  cancelWebWorkspaceFileChooser,
  fetchWebWorkspaceFileChooser,
} from '../api'
import { useFileChooserBridge } from '../hooks/use-file-chooser-bridge'

vi.mock('../api', () => ({
  fetchWebWorkspaceFileChooser: vi.fn(),
  uploadWebWorkspaceFileChooserFiles: vi.fn(),
  cancelWebWorkspaceFileChooser: vi.fn(),
}))

const fetchChooser = vi.mocked(fetchWebWorkspaceFileChooser)
const cancelChooser = vi.mocked(cancelWebWorkspaceFileChooser)

function Harness() {
  const controller = useFileChooserBridge({ sessionId: 'session-1', enabled: true })
  return (
    <div>
      {controller.errorMessageKey ? (
        <span role='alert'>{controller.errorMessageKey}</span>
      ) : null}
    </div>
  )
}

beforeEach(() => {
  cancelChooser.mockResolvedValue({
    chooser_id: 'chooser-0000000000000001',
    state: 'CANCELLED',
    error: '',
    updated_at: 2,
  })
})
afterEach(() => {
  cleanup()
  document.querySelectorAll('input[type="file"]').forEach((input) => input.remove())
  vi.restoreAllMocks()
})

describe('useFileChooserBridge', () => {
  test('opens a picker for a pending chooser', async () => {
    fetchChooser.mockResolvedValueOnce({
      chooser_id: 'chooser-0000000000000001',
      mode: 'selectMultiple',
      created_at: 1,
      expires_at: 999,
    })
    fetchChooser.mockImplementation(() => new Promise(() => undefined))
    const click = vi
      .spyOn(HTMLInputElement.prototype, 'click')
      .mockImplementation(() => undefined)

    render(<Harness />)

    await waitFor(() => expect(click).toHaveBeenCalledTimes(1))
    const input = document.querySelector('input[type="file"]')
    expect(input).not.toBeNull()
    expect((input as HTMLInputElement).multiple).toBe(true)
  })

  test('cancels the guard command when the picker is dismissed', async () => {
    fetchChooser.mockResolvedValueOnce({
      chooser_id: 'chooser-0000000000000001',
      mode: 'selectSingle',
      created_at: 1,
      expires_at: 999,
    })
    fetchChooser.mockImplementation(() => new Promise(() => undefined))
    vi.spyOn(HTMLInputElement.prototype, 'click').mockImplementation(
      () => undefined
    )
    cancelChooser.mockResolvedValue({
      chooser_id: 'chooser-0000000000000001',
      state: 'CANCELLED',
      error: '',
      updated_at: 2,
    })

    render(<Harness />)
    await waitFor(() =>
      expect(document.querySelector('input[type="file"]')).not.toBeNull()
    )
    const input = document.querySelector('input[type="file"]')
    input?.dispatchEvent(new Event('cancel'))

    await waitFor(() =>
      expect(cancelChooser).toHaveBeenCalledWith(
        'session-1',
        'chooser-0000000000000001'
      )
    )
  })

  test('renders the classified busy message', async () => {
    fetchChooser.mockRejectedValue({
      response: {
        status: 409,
        data: { code: 'WEB_WORKSPACE_FILE_BRIDGE_BUSY' },
      },
    })

    render(<Harness />)

    expect(
      await screen.findByText(
        'Another file chooser is already pending for this workspace.'
      )
    ).toBeTruthy()
  })

  test('renders the classified expired message', async () => {
    fetchChooser.mockRejectedValue({
      response: {
        status: 410,
        data: { code: 'WEB_WORKSPACE_FILE_CHOOSER_EXPIRED' },
      },
    })

    render(<Harness />)

    expect(
      await screen.findByText('The local file chooser expired. Click the paperclip again.')
    ).toBeTruthy()
  })
})
