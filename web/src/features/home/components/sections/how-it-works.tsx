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
import { Link } from '@tanstack/react-router'
import { ArrowRight, Sparkles, UserRound } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { DREAMSTARS_STEPS } from '../../constants'
import { useReducedMotion } from '../../hooks'

const STEP_ICONS = {
  user: UserRound,
  sparkles: Sparkles,
  arrow: ArrowRight,
}

interface HowItWorksProps {
  docsUrl: string
  isAuthenticated: boolean
}

export function HowItWorks(props: HowItWorksProps) {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotion()
  const sectionRef = useRef<HTMLElement>(null)
  const [revealed, setRevealed] = useState(reducedMotion)
  const docsIsExternal = props.docsUrl.startsWith('http')

  useEffect(() => {
    if (reducedMotion) {
      setRevealed(true)
      return
    }

    const section = sectionRef.current
    if (!section) return
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return
        setRevealed(true)
        observer.disconnect()
      },
      { threshold: 0.28 }
    )
    observer.observe(section)
    return () => observer.disconnect()
  }, [reducedMotion])

  return (
    <section
      ref={sectionRef}
      id='getting-started'
      className='dreamstars-steps-section'
      data-revealed={revealed}
    >
      <div className='dreamstars-steps-heading'>
        <span className='dreamstars-kicker'>{t('Get started')}</span>
        <h2>{t('Three steps to begin your AI journey')}</h2>
        <p>
          {t(
            'No complex configuration research is needed. Start with these clear steps.'
          )}
        </p>
      </div>

      <div className='dreamstars-steps'>
        <svg
          className='dreamstars-step-track'
          viewBox='0 0 1200 140'
          preserveAspectRatio='none'
          aria-hidden='true'
        >
          <defs>
            <linearGradient id='dreamstars-track-gradient' x1='0' x2='1'>
              <stop offset='0%' stopColor='#168cff' />
              <stop offset='52%' stopColor='#4a68ff' />
              <stop offset='100%' stopColor='#bc54ff' />
            </linearGradient>
          </defs>
          <path
            pathLength={1}
            d='M 0 82 C 160 20, 280 20, 420 82 S 680 144, 800 82 S 1040 20, 1200 82'
          />
        </svg>

        {DREAMSTARS_STEPS.map((step, index) => {
          const Icon = STEP_ICONS[step.icon]
          return (
            <article
              key={step.number}
              style={{ '--step-index': index } as React.CSSProperties}
            >
              <div className='dreamstars-step-number'>{step.number}</div>
              <Icon aria-hidden='true' />
              <h3>{t(step.title)}</h3>
              <p>{t(step.description)}</p>
            </article>
          )
        })}
      </div>

      <div className='dreamstars-step-actions'>
        <Button
          size='lg'
          render={
            <Link to={props.isAuthenticated ? '/dashboard' : '/sign-in'} />
          }
        >
          {props.isAuthenticated
            ? t('Enter Dashboard')
            : t('Sign in to the platform')}
          <ArrowRight aria-hidden='true' />
        </Button>
        <Button
          size='lg'
          variant='outline'
          render={
            docsIsExternal ? (
              <a
                href={props.docsUrl}
                target='_blank'
                rel='noopener noreferrer'
              />
            ) : (
              <Link to={props.docsUrl} />
            )
          }
        >
          {t('Use Guide')}
        </Button>
      </div>
    </section>
  )
}
