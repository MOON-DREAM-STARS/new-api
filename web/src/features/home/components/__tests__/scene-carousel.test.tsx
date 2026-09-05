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
import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { SceneCarousel } from '../scene-carousel'

function setReducedMotion(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)' && matches,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  )
}

describe('SceneCarousel', () => {
  beforeEach(() => {
    setReducedMotion(false)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('supports arrow buttons, dots, and keyboard navigation', async () => {
    const user = userEvent.setup()
    render(<SceneCarousel />)

    expect(
      screen.getByRole('heading', { name: 'Research and Literature' })
    ).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Next scenario' }))
    expect(screen.getByRole('heading', { name: 'Data and Code' })).toBeVisible()

    await user.click(
      screen.getByRole('button', {
        name: 'Show scenario 4: Learning and Writing',
      })
    )
    expect(
      screen.getByRole('heading', { name: 'Learning and Writing' })
    ).toBeVisible()

    const carousel = screen.getByRole('region', {
      name: 'Campus AI application scenarios',
    })
    fireEvent.keyDown(carousel, { key: 'ArrowLeft' })
    expect(
      screen.getByRole('heading', { name: 'Presentation and Communication' })
    ).toBeVisible()
  })

  it('shows the selected decorative scene illustration without duplicate accessible text', async () => {
    const user = userEvent.setup()
    const { container } = render(<SceneCarousel />)

    const researchArt = container.querySelector(
      ".dreamstars-scene-art[data-scene='research']"
    )
    expect(researchArt).toHaveAttribute('data-active', 'true')
    expect(researchArt?.querySelector('img')).toHaveAttribute('alt', '')
    expect(
      screen.getByText('Use GPT to read academic literature faster.')
    ).toBeVisible()
    expect(
      screen.queryByText(
        'Paper review · Literature mapping · Research directions'
      )
    ).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('button', {
        name: 'Show scenario 2: Data and Code',
      })
    )
    const dataArt = container.querySelector(
      ".dreamstars-scene-art[data-scene='data']"
    )
    expect(dataArt).toHaveAttribute('data-active', 'true')
    expect(dataArt?.querySelector('img')).toHaveAttribute('alt', '')
  })

  it('pauses automatic advance during hover and resumes after leaving', () => {
    vi.useFakeTimers()
    render(<SceneCarousel />)
    const carousel = screen.getByRole('region', {
      name: 'Campus AI application scenarios',
    })

    fireEvent.mouseEnter(carousel)
    act(() => vi.advanceTimersByTime(14000))
    expect(
      screen.getByRole('heading', { name: 'Research and Literature' })
    ).toBeVisible()

    fireEvent.mouseLeave(carousel)
    act(() => vi.advanceTimersByTime(7000))
    expect(screen.getByRole('heading', { name: 'Data and Code' })).toBeVisible()
  })

  it('stops automatic advance when reduced motion is requested', () => {
    vi.useFakeTimers()
    setReducedMotion(true)
    render(<SceneCarousel />)
    const carousel = screen.getByRole('region', {
      name: 'Campus AI application scenarios',
    })

    expect(carousel).toHaveAttribute('data-autoplay', 'paused')
    act(() => vi.advanceTimersByTime(14000))
    expect(
      screen.getByRole('heading', { name: 'Research and Literature' })
    ).toBeVisible()
  })
})
