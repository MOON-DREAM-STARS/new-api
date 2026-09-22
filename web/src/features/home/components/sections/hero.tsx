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
import {
  ArrowRight,
  BadgeDollarSign,
  GraduationCap,
  RefreshCcw,
  ShieldCheck,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { SceneCarousel } from '../scene-carousel'

interface HeroProps {
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()

  return (
    <section id='dreamstars-home' className='dreamstars-hero'>
      <div className='dreamstars-stars' aria-hidden='true' />
      <div className='dreamstars-hero-inner'>
        <div className='dreamstars-hero-copy'>
          <div className='dreamstars-badge'>
            <GraduationCap aria-hidden='true' />
            {t('AI model platform for university faculty and students')}
          </div>

          <h1>
            <span>{t('Transparent CNY billing')}</span>
            <strong>{t('Pay only for what you use')}</strong>
          </h1>

          <p className='dreamstars-hero-model-coverage'>
            <Sparkles aria-hidden='true' />
            <span>
              {t(
                'Supports GPT, Claude, Grok, DeepSeek, and other leading domestic and international models, starting at 0.1× official prices.'
              )}
            </span>
          </p>

          <div className='dreamstars-hero-actions'>
            <Button size='lg' render={<Link to='/pricing' />}>
              {t('View models and CNY prices')}
              <ArrowRight aria-hidden='true' />
            </Button>
            <Button
              size='lg'
              variant='outline'
              render={<a href='#getting-started' />}
            >
              {t('Use Guide')}
            </Button>
            <Button
              size='lg'
              variant='outline'
              render={
                <Link to={props.isAuthenticated ? '/dashboard' : '/sign-in'} />
              }
            >
              {props.isAuthenticated ? t('Enter Dashboard') : t('Sign in')}
            </Button>
          </div>

          <div className='dreamstars-principles'>
            <span>{t('Transparent billing principles')}</span>
            <div>
              <p>
                <BadgeDollarSign aria-hidden='true' />
                {t('Direct CNY pricing')}
              </p>
              <p>
                <RefreshCcw aria-hidden='true' />
                {t('Fixed exchange rate: 1 USD = 7 CNY')}
              </p>
              <p>
                <ShieldCheck aria-hidden='true' />
                {t('No hidden multipliers')}
              </p>
            </div>
          </div>
        </div>

        <SceneCarousel />
      </div>
    </section>
  )
}
