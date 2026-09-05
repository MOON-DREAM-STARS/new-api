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
import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { DREAMSTARS_MODEL_BRANDS } from '../constants'
import { useReducedMotion } from '../hooks'

type OrbitStyle = CSSProperties & {
  '--orbit-delay': string
  '--static-x': string
  '--static-y': string
}

const PRIMARY_POSITIONS = [
  ['15%', '50.5%'],
  ['25%', '35.5%'],
  ['43%', '27.5%'],
  ['65%', '28.5%'],
  ['83%', '40.5%'],
  ['82%', '60.5%'],
  ['64%', '73.5%'],
  ['39%', '74.5%'],
  ['22%', '64.5%'],
] as const

export function ModelOrbit() {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotion()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const [pageVisible, setPageVisible] = useState(!document.hidden)
  const [inViewport, setInViewport] = useState(false)
  const orbitRef = useRef<HTMLDivElement>(null)
  const primaryBrands = DREAMSTARS_MODEL_BRANDS.filter(
    (brand) => brand.tier === 'primary'
  )

  useEffect(() => {
    const handleVisibility = () => setPageVisible(!document.hidden)
    document.addEventListener('visibilitychange', handleVisibility)
    return () =>
      document.removeEventListener('visibilitychange', handleVisibility)
  }, [])

  useEffect(() => {
    const orbit = orbitRef.current
    if (!orbit || reducedMotion) return
    if (!('IntersectionObserver' in window)) {
      setInViewport(true)
      return
    }

    const observer = new IntersectionObserver(
      ([entry]) => setInViewport(entry.isIntersecting),
      { threshold: 0.01 }
    )
    observer.observe(orbit)
    return () => observer.disconnect()
  }, [reducedMotion])

  const orbitIsRunning = pageVisible && inViewport && !reducedMotion
  let motionState = 'paused'
  if (reducedMotion) {
    motionState = 'reduced'
  } else if (orbitIsRunning) {
    motionState = 'animated'
  }

  return (
    <div
      ref={orbitRef}
      className={cn('dreamstars-model-orbit', !orbitIsRunning && 'is-paused')}
      data-motion={motionState}
      aria-label={t('Model ecosystem orbit')}
    >
      <div className='dreamstars-orbit-halo' aria-hidden='true'>
        <span className='dreamstars-orbit-ring dreamstars-orbit-ring-rear' />
        <span className='dreamstars-orbit-ring dreamstars-orbit-ring-core' />
        <span className='dreamstars-orbit-ring dreamstars-orbit-ring-front' />
      </div>

      {primaryBrands.map((brand, index) => {
        const distance =
          activeIndex === null
            ? Number.POSITIVE_INFINITY
            : Math.min(
                Math.abs(activeIndex - index),
                primaryBrands.length - Math.abs(activeIndex - index)
              )
        const position = PRIMARY_POSITIONS[index]
        const style: OrbitStyle = {
          '--orbit-delay': `${(-34 * (index + 0.35)) / primaryBrands.length}s`,
          '--static-x': position[0],
          '--static-y': position[1],
        }

        return (
          <div
            key={brand.name}
            className='dreamstars-orbit-item dreamstars-orbit-item-primary'
            style={style}
          >
            <div
              className='dreamstars-orbit-brand'
              data-active={distance === 0}
              data-neighbor={distance === 1}
              role='img'
              tabIndex={0}
              aria-label={t('Model ecosystem brand: {{brand}}', {
                brand: t(brand.name),
              })}
              onMouseEnter={() => setActiveIndex(index)}
              onMouseLeave={() => setActiveIndex(null)}
              onFocus={() => setActiveIndex(index)}
              onBlur={() => setActiveIndex(null)}
            >
              <span aria-hidden='true'>{getLobeIcon(brand.iconName, 38)}</span>
              <strong>{t(brand.name)}</strong>
            </div>
          </div>
        )
      })}

      <div className='dreamstars-orbit-center' aria-hidden='true'>
        <span>{t('More models are continuously being added')}</span>
      </div>
    </div>
  )
}
