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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  copyWebWorkspaceClipboard,
  pasteWebWorkspaceClipboard,
} from '../api'
import { WorkspaceToolbar } from '../components/workspace-toolbar'
import { useClipboardBridge } from '../hooks/use-clipboard-bridge'

vi.mock('../api', () => ({
  copyWebWorkspaceClipboard: vi.fn(),
  pasteWebWorkspaceClipboard: vi.fn(),
}))

const copyClipboard = vi.mocked(copyWebWorkspaceClipboard)
const pasteClipboard = vi.mocked(pasteWebWorkspaceClipboard)

function noop() {}

function installClipboard(value: Partial<Clipboard>) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value,
  })
}

function Harness() {
  const surfaceRef = useRef<HTMLDivElement>(null)
  const bridge = useClipboardBridge({
    sessionId: 'session-1',
    enabled: true,
    containerRef: surfaceRef,
  })

  return (
    <div>
      <div
        ref={surfaceRef}
        data-testid='remote-surface'
        tabIndex={0}
        aria-label='Remote browser display'
      />
      <input aria-label='Local input' />
      {bridge.errorMessageKey ? (
        <span role='alert'>{bridge.errorMessageKey}</span>
      ) : null}
      <WorkspaceToolbar
        connection='connected'
        connectionDetail=''
        sessionMode='LOCKED'
        isLive
        isBusy={false}
        navigation={null}
        isNavigationAvailable
        isNavigationPending={false}
        inputMode='remote'
        inputEnabled
        immersive={false}
        onStart={noop}
        onReconnect={noop}
        onRestart={noop}
        onLockSession={noop}
        onStop={noop}
        onNavigateBack={noop}
        onNavigateForward={noop}
        onNavigateReload={noop}
        onCopyRemoteSelection={() => {
          void bridge.copyRemoteSelection()
        }}
        onPasteIntoRemote={() => {
          void bridge.pasteIntoRemote()
        }}
        onToggleImmersive={noop}
        onToggleInputMode={noop}
      />
    </div>
  )
}

function IframeHarness() {
  const surfaceRef = useRef<HTMLDivElement>(null)
  const bridge = useClipboardBridge({
    sessionId: 'session-1',
    enabled: true,
    containerRef: surfaceRef,
  })

  return (
    <div ref={surfaceRef} data-testid='remote-surface'>
      <iframe data-testid='remote-frame' title='Remote frame' />
      {bridge.errorMessageKey ? (
        <span role='alert'>{bridge.errorMessageKey}</span>
      ) : null}
    </div>
  )
}

afterEach(() => {
  copyClipboard.mockReset()
  pasteClipboard.mockReset()
  vi.restoreAllMocks()
})

describe('useClipboardBridge', () => {
  test('intercepts Ctrl/Cmd shortcuts only while the remote surface has focus', async () => {
    copyClipboard.mockResolvedValue({
      mime: 'text/plain',
      data: new Blob(['remote'], { type: 'text/plain' }),
    })
    pasteClipboard.mockResolvedValue(undefined)
    installClipboard({
      writeText: vi.fn().mockResolvedValue(undefined),
      read: vi.fn().mockResolvedValue([
        {
          types: ['text/plain'],
          getType: vi.fn().mockResolvedValue(new Blob(['local'], { type: 'text/plain' })),
        },
      ]) as unknown as Clipboard['read'],
    })

    render(<Harness />)
    const remote = screen.getByTestId('remote-surface')
    const localInput = screen.getByRole('textbox', { name: 'Local input' })

    remote.focus()
    fireEvent.keyDown(remote, { key: 'c', ctrlKey: true })
    await waitFor(() => expect(copyClipboard).toHaveBeenCalledTimes(1))
    fireEvent.keyDown(remote, { key: 'v', metaKey: true })
    await waitFor(() => expect(pasteClipboard).toHaveBeenCalledTimes(1))

    localInput.focus()
    fireEvent.keyDown(localInput, { key: 'c', ctrlKey: true })
    await waitFor(() => expect(copyClipboard).toHaveBeenCalledTimes(1))
  })

  test('intercepts shortcuts inside the same-origin Kasm iframe', async () => {
    copyClipboard.mockResolvedValue({
      mime: 'text/plain',
      data: new Blob(['remote'], { type: 'text/plain' }),
    })
    installClipboard({
      writeText: vi.fn().mockResolvedValue(undefined),
    })

    render(<IframeHarness />)
    const frame = screen.getByTestId('remote-frame') as HTMLIFrameElement
    await waitFor(() => expect(frame.contentDocument).toBeTruthy())
    frame.focus()
    const event = new KeyboardEvent('keydown', {
      key: 'c',
      ctrlKey: true,
      bubbles: true,
    })
    frame.contentDocument!.dispatchEvent(event)
    await waitFor(() => expect(copyClipboard).toHaveBeenCalledTimes(1))
  })

  test('renders the permission notice and keeps toolbar actions available', async () => {
    copyClipboard.mockResolvedValue({
      mime: 'text/plain',
      data: new Blob(['remote'], { type: 'text/plain' }),
    })
    const read = vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError'))
    installClipboard({
      writeText: vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError')),
      read: read as unknown as Clipboard['read'],
      readText: vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError')),
    })

    render(<Harness />)

    fireEvent.click(screen.getByRole('button', { name: 'Copy remote selection' }))
    expect(
      await screen.findByText(
        'Clipboard access was denied. Grant permission and try again.'
      )
    ).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Paste into remote' }))
    await waitFor(() => expect(read).toHaveBeenCalledTimes(1))
  })
})
