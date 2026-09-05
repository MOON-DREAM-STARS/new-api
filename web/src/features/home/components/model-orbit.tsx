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
import { useEffect, useState, type CSSProperties } from 'react'
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
  ['17%', '31%'],
  ['39%', '23%'],
  ['62%', '27%'],
  ['82%', '38%'],
  ['79%', '68%'],
  ['58%', '77%'],
  ['35%', '76%'],
  ['15%', '62%'],
] as const

const SECONDARY_POSITIONS = [
  ['50%', '10%'],
  ['20%', '84%'],
  ['51%', '91%'],
  ['82%', '84%'],
] as const

export function ModelOrbit() {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotion()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const [pageVisible, setPageVisible] = useState(!document.hidden)
  const primaryBrands = DREAMSTARS_MODEL_BRANDS.filter(
    (brand) => brand.tier === 'primary'
  )
  const secondaryBrands = DREAMSTARS_MODEL_BRANDS.filter(
    (brand) => brand.tier === 'secondary'
  )

  useEffect(() => {
    const handleVisibility = () => setPageVisible(!document.hidden)
    document.addEventListener('visibilitychange', handleVisibility)
    return () =>
      document.removeEventListener('visibilitychange', handleVisibility)
  }, [])

  return (
    <div
      className={cn(
        'dreamstars-model-orbit',
        (!pageVisible || reducedMotion) && 'is-paused'
      )}
      data-motion={reducedMotion ? 'reduced' : 'animated'}
      aria-label={t('Model ecosystem orbit')}
    >
      <div className='dreamstars-orbit-halo' aria-hidden='true'>
        <span />
        <span />
        <span />
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
          '--orbit-delay': `${(-34 * index) / primaryBrands.length}s`,
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

      {secondaryBrands.map((brand, index) => {
        const position = SECONDARY_POSITIONS[index]
        const style: OrbitStyle = {
          '--orbit-delay': `${(-34 * (index + 0.25)) / secondaryBrands.length}s`,
          '--static-x': position[0],
          '--static-y': position[1],
        }
        return (
          <div
            key={brand.name}
            className='dreamstars-orbit-item dreamstars-orbit-item-secondary'
            style={style}
          >
            <div
              className='dreamstars-orbit-brand'
              role='img'
              tabIndex={0}
              aria-label={t('Expanding model ecosystem brand: {{brand}}', {
                brand: t(brand.name),
              })}
            >
              <span aria-hidden='true'>{getLobeIcon(brand.iconName, 24)}</span>
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
