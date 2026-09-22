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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { UpdateCheckerSection } from '../update-checker-section'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
  },
}))

const availableReleaseResponse = {
  success: true,
  message: '',
  data: {
    current_version: 'dreamstars-20260904-120000-aaaaaaa',
    status: 'update_available' as const,
    source: 'live' as const,
    checked_at: '2026-09-05T12:00:00Z',
    latest: {
      release_tag: 'dreamstars-20260905-120000-123456789abc',
      version: 'dreamstars-20260905-120000-123456789abc',
      commit: '0123456789abcdef0123456789abcdef01234567',
      published_at: '2026-09-05T12:00:00Z',
      release_url:
        'https://github.com/MOON-DREAM-STARS/new-api/releases/tag/dreamstars-20260905-120000-123456789abc',
      image: 'ghcr.io/moon-dream-stars/new-api',
      digest:
        'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
      platforms: ['linux/amd64'],
    },
  },
}

describe('UpdateCheckerSection', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
  })

  it('checks the fixed local Fork endpoint and presents immutable release metadata', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: availableReleaseResponse,
    } as never)
    const user = userEvent.setup()

    render(
      <UpdateCheckerSection
        currentVersion='dreamstars-20260904-120000-aaaaaaa'
        startTime={Date.now()}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(api.get).toHaveBeenCalledWith('/api/update/release')
    expect(
      await screen.findByText(
        'An approved Dreamstars fork release is available.'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByText('0123456789abcdef0123456789abcdef01234567')
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'This page only checks Dreamstars fork releases. It never deploys or restarts the server.'
      )
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /deploy/i })
    ).not.toBeInTheDocument()
  })

  it('opens the trusted release URL without offering a deployment action', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: availableReleaseResponse,
    } as never)
    const openWindow = vi.spyOn(window, 'open').mockImplementation(() => null)
    const user = userEvent.setup()

    render(<UpdateCheckerSection />)
    await user.click(screen.getByRole('button', { name: 'Check for updates' }))
    await user.click(
      await screen.findByRole('button', { name: 'Open release' })
    )

    expect(openWindow).toHaveBeenCalledWith(
      availableReleaseResponse.data.latest.release_url,
      '_blank',
      'noopener,noreferrer'
    )
    expect(
      screen.queryByRole('button', { name: /docker/i })
    ).not.toBeInTheDocument()
  })

  it('shows unavailable and stale states returned by the local service', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        success: true,
        message: '',
        data: {
          current_version: 'dreamstars-20260904-120000-aaaaaaa',
          status: 'unavailable' as const,
          source: 'unavailable' as const,
          checked_at: '0001-01-01T00:00:00Z',
        },
      },
    } as never)
    const user = userEvent.setup()

    render(<UpdateCheckerSection />)
    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(
      await screen.findByText(
        'No approved Dreamstars fork release is currently available.'
      )
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Open release' })
    ).not.toBeInTheDocument()
  })

  it('shows the latest-version state only when the local service confirms it', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        ...availableReleaseResponse,
        data: {
          ...availableReleaseResponse.data,
          status: 'up_to_date' as const,
          source: 'live' as const,
        },
      },
    } as never)
    const user = userEvent.setup()

    render(<UpdateCheckerSection />)
    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(
      await screen.findByText('You are running the latest Dreamstars release.')
    ).toBeInTheDocument()
    expect(
      screen.queryByText('Showing stale cached release information.')
    ).not.toBeInTheDocument()
  })

  it('shows an unavailable live check and stale-cache warning without implying a deployment', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        ...availableReleaseResponse,
        data: {
          ...availableReleaseResponse.data,
          status: 'unavailable' as const,
          source: 'stale' as const,
        },
      },
    } as never)
    const user = userEvent.setup()

    render(<UpdateCheckerSection />)
    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(
      await screen.findByText(
        'Live release check is unavailable; showing previously verified release information.'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByText('Showing stale cached release information.')
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /deploy/i })
    ).not.toBeInTheDocument()
  })
})
