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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ModelOrbit } from '../model-orbit'

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (name: string) => <svg data-icon-name={name} />,
}))

const observerState = vi.hoisted(() => ({
  callback: undefined as IntersectionObserverCallback | undefined,
}))

class IntersectionObserverMock {
  constructor(callback: IntersectionObserverCallback) {
    observerState.callback = callback
  }

  disconnect(): void {}
  observe(): void {}
}

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

describe('ModelOrbit', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', IntersectionObserverMock)
  })

  afterEach(() => {
    observerState.callback = undefined
    vi.unstubAllGlobals()
  })

  it('gives every brand an accessible name and keyboard focus feedback', () => {
    setReducedMotion(false)
    render(<ModelOrbit />)

    const brands = screen.getAllByRole('img', {
      name: /model ecosystem brand/i,
    })
    expect(brands).toHaveLength(9)
    for (const brand of brands) expect(brand).toHaveAttribute('tabindex', '0')

    expect(
      screen.getByRole('img', {
        name: 'Model ecosystem brand: MiniMax',
      })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: /Llama/i })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: /Mistral/i })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: /01\.AI Yi/i })
    ).not.toBeInTheDocument()

    const gemini = screen.getByRole('img', {
      name: 'Model ecosystem brand: Gemini',
    })
    fireEvent.focus(gemini)
    expect(gemini).toHaveAttribute('data-active', 'true')
    fireEvent.blur(gemini)
    expect(gemini).toHaveAttribute('data-active', 'false')
  })

  it('keeps its three-dimensional orbit layers decorative', () => {
    setReducedMotion(false)
    const { container } = render(<ModelOrbit />)

    const halo = container.querySelector('.dreamstars-orbit-halo')
    expect(halo).toHaveAttribute('aria-hidden', 'true')
    expect(halo?.querySelectorAll('.dreamstars-orbit-ring')).toHaveLength(3)
  })

  it('marks the orbit as static when reduced motion is requested', () => {
    setReducedMotion(true)
    render(<ModelOrbit />)

    expect(screen.getByLabelText('Model ecosystem orbit')).toHaveAttribute(
      'data-motion',
      'reduced'
    )
  })

  it('runs only while the orbit is inside the viewport', () => {
    setReducedMotion(false)
    render(<ModelOrbit />)

    const orbit = screen.getByLabelText('Model ecosystem orbit')
    expect(orbit).toHaveAttribute('data-motion', 'paused')
    expect(observerState.callback).toBeDefined()

    act(() => {
      observerState.callback?.(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        {} as IntersectionObserver
      )
    })
    expect(orbit).toHaveAttribute('data-motion', 'animated')

    act(() => {
      observerState.callback?.(
        [{ isIntersecting: false } as IntersectionObserverEntry],
        {} as IntersectionObserver
      )
    })
    expect(orbit).toHaveAttribute('data-motion', 'paused')
  })
})
