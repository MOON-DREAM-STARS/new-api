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
import type { ComponentPropsWithoutRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { Home } from '../index'

const testState = vi.hoisted(() => ({
  authenticated: false,
  homeContent: { content: '', isLoaded: true, isUrl: false },
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (
    props: ComponentPropsWithoutRef<'a'> & { to: string; disabled?: boolean }
  ) => {
    const { to, disabled: _disabled, ...anchorProps } = props
    return <a href={to} {...anchorProps} />
  },
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => (
    <div data-testid='public-layout'>{props.children}</div>
  ),
}))

vi.mock('@/components/rich-content', () => ({
  RichContent: (props: { content: string }) => (
    <div data-testid='custom-home-content'>{props.content}</div>
  ),
}))

vi.mock('@/context/theme-provider', () => ({
  useTheme: () => ({ resolvedTheme: 'dark' }),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { docs_link: 'https://docs.example.test' } }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (name: string) => <svg data-icon-name={name} />,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: () => ({
    auth: { user: testState.authenticated ? { id: 1 } : null },
  }),
}))

vi.mock('../hooks', () => ({
  useHomePageContent: () => testState.homeContent,
  useReducedMotion: () => true,
}))

class IntersectionObserverMock {
  observe(): void {}
  disconnect(): void {}
}

vi.stubGlobal('IntersectionObserver', IntersectionObserverMock)

describe('Home default and custom content branches', () => {
  beforeEach(() => {
    testState.authenticated = false
    testState.homeContent = { content: '', isLoaded: true, isUrl: false }
  })

  it('shows sign-in and pricing without exposing a registration entry', () => {
    render(<Home />)

    expect(screen.getByRole('button', { name: 'Sign in' })).toHaveAttribute(
      'href',
      '/sign-in'
    )
    expect(
      screen.getByRole('button', { name: /View models and CNY prices/ })
    ).toHaveAttribute('href', '/pricing')
    expect(document.querySelector('a[href="/sign-up"]')).not.toBeInTheDocument()
  })

  it('keeps the authenticated dashboard entry', () => {
    testState.authenticated = true
    render(<Home />)

    expect(
      screen.getByRole('button', { name: 'Enter Dashboard' })
    ).toHaveAttribute('href', '/dashboard')
  })

  it('renders custom HomePageContent instead of the default homepage', () => {
    testState.homeContent = {
      content: '# Custom homepage',
      isLoaded: true,
      isUrl: false,
    }
    render(<Home />)

    expect(screen.getByTestId('custom-home-content')).toHaveTextContent(
      '# Custom homepage'
    )
    expect(
      screen.queryByRole('heading', { name: 'Transparent CNY billing' })
    ).not.toBeInTheDocument()
  })
})
