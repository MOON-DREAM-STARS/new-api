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
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '../config'
import { SidebarModulesSection } from '../sidebar-modules-section'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('legacy settings stay global until recommended roles are explicitly saved; role switches persist independently', async () => {
  const config = parseSidebarModulesAdmin('')
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  function Fixture() {
    const [container, setContainer] = useState<HTMLDivElement | null>(null)
    return (
      <>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <SidebarModulesSection
            config={config}
            initialSerialized={serializeSidebarModulesAdmin(config)}
          />
        </SettingsPageProvider>
      </>
    )
  }
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>
  )
  expect(screen.queryByRole('tab', { name: 'Common' })).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('button', { name: 'Apply recommended role defaults' })
  )
  expect(put).not.toHaveBeenCalled()
  expect(screen.getByRole('switch', { name: 'Audit Logs' })).not.toBeChecked()
  expect(screen.getByRole('switch', { name: 'Usage logs' })).toBeChecked()
  await user.click(screen.getByRole('tab', { name: 'Admin' }))
  expect(screen.getByRole('switch', { name: 'Audit Logs' })).toBeChecked()
  await user.click(screen.getByRole('switch', { name: 'Security & Access' }))
  await user.click(screen.getByRole('button', { name: 'Save sidebar modules' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  const body = put.mock.calls[0][1] as { value: string }
  const saved = parseSidebarModulesAdmin(body.value)
  expect(saved.console.audit).toBe(true)
  expect(saved.roles?.['1'].console.audit).toBe(false)
  expect(saved.roles?.['1'].console.midjourney).toBe(false)
  expect(saved.roles?.['1'].console.task).toBe(false)
  expect(saved.roles?.['1'].console.log).toBe(true)
  expect(saved.roles?.['10'].personal.security).toBe(false)
  expect(saved.roles?.['100'].personal.security).toBe(true)
})
