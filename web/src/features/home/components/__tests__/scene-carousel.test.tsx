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
    const { container } = render(<SceneCarousel />)

    const activeCopy = () =>
      container.querySelector(".dreamstars-scene-copy-item[data-active='true']")
    expect(activeCopy()).toHaveTextContent(
      'Use GPT to read academic literature faster.'
    )

    await user.click(screen.getByRole('button', { name: 'Next scenario' }))
    expect(activeCopy()).toHaveTextContent(
      'Use DeepSeek to work through data and code more efficiently.'
    )

    await user.click(
      screen.getByRole('button', {
        name: 'Show scenario 4: Learning and Writing',
      })
    )
    expect(activeCopy()).toHaveTextContent(
      'Use Claude to understand concepts and support your learning and writing.'
    )

    const carousel = screen.getByRole('region', {
      name: 'Campus AI application scenarios',
    })
    fireEvent.keyDown(carousel, { key: 'ArrowLeft' })
    expect(activeCopy()).toHaveTextContent(
      'Use Gemini to organize key points and communicate your findings.'
    )
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

  it('keeps only the active illustration and its exiting transition layer mounted', () => {
    vi.useFakeTimers()
    const { container } = render(<SceneCarousel />)

    const renderedArt = () =>
      container.querySelectorAll('.dreamstars-scene-art')
    expect(renderedArt()).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Next scenario' }))
    expect(renderedArt()).toHaveLength(2)
    expect(
      container.querySelector(".dreamstars-scene-art[data-scene='research']")
    ).toHaveAttribute('data-exiting', 'true')
    expect(
      container.querySelector(".dreamstars-scene-art[data-scene='data']")
    ).toHaveAttribute('data-active', 'true')

    act(() => vi.advanceTimersByTime(700))
    expect(renderedArt()).toHaveLength(1)
    expect(
      container.querySelector(".dreamstars-scene-art[data-scene='data']")
    ).toHaveAttribute('data-active', 'true')
  })

  it('prefetches only the next illustration during browser idle time', () => {
    const prefetchedSources: string[] = []
    class ImageMock {
      decoding = ''

      set src(value: string) {
        prefetchedSources.push(value)
      }
    }

    vi.stubGlobal('Image', ImageMock)
    vi.stubGlobal('requestIdleCallback', (callback: () => void) => {
      callback()
      return 1
    })
    vi.stubGlobal('cancelIdleCallback', vi.fn())
    render(<SceneCarousel />)

    expect(prefetchedSources).toHaveLength(1)
    expect(prefetchedSources[0]).toContain(
      'dreamstars-scene-data-deepseek.webp'
    )
  })

  it('pauses automatic advance during hover and resumes after leaving', () => {
    vi.useFakeTimers()
    render(<SceneCarousel />)
    const carousel = screen.getByRole('region', {
      name: 'Campus AI application scenarios',
    })
    expect(
      screen.queryByRole('button', { name: /pause carousel|resume carousel/i })
    ).not.toBeInTheDocument()

    fireEvent.mouseEnter(carousel)
    act(() => vi.advanceTimersByTime(14000))
    expect(
      screen.getByText('Use GPT to read academic literature faster.')
    ).toBeVisible()

    fireEvent.mouseLeave(carousel)
    act(() => vi.advanceTimersByTime(7000))
    expect(
      screen.getByText(
        'Use DeepSeek to work through data and code more efficiently.'
      )
    ).toBeVisible()
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
      screen.getByText('Use GPT to read academic literature faster.')
    ).toBeVisible()
  })
})
