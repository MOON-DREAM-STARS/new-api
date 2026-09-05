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
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ModelOrbit } from '../model-orbit'

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (name: string) => <svg data-icon-name={name} />,
}))

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
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('gives every brand an accessible name and keyboard focus feedback', () => {
    setReducedMotion(false)
    render(<ModelOrbit />)

    const brands = screen.getAllByRole('img', {
      name: /model ecosystem brand/i,
    })
    expect(brands).toHaveLength(12)
    for (const brand of brands) expect(brand).toHaveAttribute('tabindex', '0')

    const gemini = screen.getByRole('img', {
      name: 'Model ecosystem brand: Gemini',
    })
    fireEvent.focus(gemini)
    expect(gemini).toHaveAttribute('data-active', 'true')
    fireEvent.blur(gemini)
    expect(gemini).toHaveAttribute('data-active', 'false')
  })

  it('marks the orbit as static when reduced motion is requested', () => {
    setReducedMotion(true)
    render(<ModelOrbit />)

    expect(screen.getByLabelText('Model ecosystem orbit')).toHaveAttribute(
      'data-motion',
      'reduced'
    )
  })
})
