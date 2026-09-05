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
import { ChevronLeft, ChevronRight, Pause, Play } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DREAMSTARS_SCENES } from '../constants'
import { useReducedMotion } from '../hooks'

const AUTO_ADVANCE_MS = 7000
const SWIPE_THRESHOLD_PX = 44

type SceneId = (typeof DREAMSTARS_SCENES)[number]['id']

const SCENE_ART = {
  research: {
    src: new URL('../assets/dreamstars-scene-research-gpt.png', import.meta.url)
      .href,
    width: 1672,
    height: 941,
  },
  data: {
    src: new URL(
      '../assets/dreamstars-scene-data-deepseek.png',
      import.meta.url
    ).href,
    width: 1672,
    height: 941,
  },
  presentation: {
    src: new URL(
      '../assets/dreamstars-scene-presentation-gemini.png',
      import.meta.url
    ).href,
    width: 1672,
    height: 941,
  },
  learning: {
    src: new URL(
      '../assets/dreamstars-scene-learning-claude.png',
      import.meta.url
    ).href,
    width: 1536,
    height: 1024,
  },
} as const satisfies Record<
  SceneId,
  { src: string; width: number; height: number }
>

export function SceneCarousel() {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotion()
  const [activeIndex, setActiveIndex] = useState(0)
  const [hovered, setHovered] = useState(false)
  const [focusWithin, setFocusWithin] = useState(false)
  const [userPaused, setUserPaused] = useState(false)
  const [pageVisible, setPageVisible] = useState(!document.hidden)
  const pointerStartX = useRef<number | null>(null)
  const paused =
    reducedMotion || hovered || focusWithin || userPaused || !pageVisible

  const selectScene = useCallback((index: number) => {
    const nextIndex =
      (index + DREAMSTARS_SCENES.length) % DREAMSTARS_SCENES.length
    setActiveIndex(nextIndex)
  }, [])

  useEffect(() => {
    const handleVisibility = () => setPageVisible(!document.hidden)
    document.addEventListener('visibilitychange', handleVisibility)
    return () =>
      document.removeEventListener('visibilitychange', handleVisibility)
  }, [])

  useEffect(() => {
    if (paused) return
    const timeoutId = window.setTimeout(() => {
      setActiveIndex((index) => (index + 1) % DREAMSTARS_SCENES.length)
    }, AUTO_ADVANCE_MS)
    return () => window.clearTimeout(timeoutId)
  }, [activeIndex, paused])

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      selectScene(activeIndex - 1)
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault()
      selectScene(activeIndex + 1)
    }
  }

  const handlePointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'mouse' || pointerStartX.current === null) return
    const distance = event.clientX - pointerStartX.current
    pointerStartX.current = null
    if (Math.abs(distance) < SWIPE_THRESHOLD_PX) return
    selectScene(distance > 0 ? activeIndex - 1 : activeIndex + 1)
  }

  return (
    <div
      className='dreamstars-carousel'
      role='region'
      aria-roledescription={t('carousel')}
      aria-label={t('Campus AI application scenarios')}
      tabIndex={0}
      data-autoplay={paused ? 'paused' : 'running'}
      onKeyDown={handleKeyDown}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocusCapture={() => setFocusWithin(true)}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) {
          setFocusWithin(false)
        }
      }}
      onPointerDown={(event) => {
        if (event.pointerType !== 'mouse') pointerStartX.current = event.clientX
      }}
      onPointerCancel={() => {
        pointerStartX.current = null
      }}
      onPointerUp={handlePointerUp}
    >
      <div className='dreamstars-carousel-glow' aria-hidden='true' />
      <div className='dreamstars-scene-visual' aria-hidden='true'>
        {DREAMSTARS_SCENES.map((scene, index) => {
          const art = SCENE_ART[scene.id]
          return (
            <div
              key={scene.id}
              className='dreamstars-scene-art'
              data-active={index === activeIndex}
              data-scene={scene.id}
            >
              <img
                src={art.src}
                width={art.width}
                height={art.height}
                alt=''
                decoding='async'
                loading={index === 0 ? 'eager' : 'lazy'}
              />
            </div>
          )
        })}
      </div>

      <div
        className='dreamstars-scene-copy'
        aria-live={paused ? 'polite' : 'off'}
      >
        {DREAMSTARS_SCENES.map((scene, index) => (
          <div
            key={scene.id}
            className='dreamstars-scene-copy-item'
            data-active={index === activeIndex}
            aria-hidden={index !== activeIndex}
          >
            <h2>{t(scene.title)}</h2>
            <p>{t(scene.description)}</p>
          </div>
        ))}
      </div>

      <div className='dreamstars-carousel-controls'>
        <span className='dreamstars-carousel-progress' aria-hidden='true'>
          <strong>{String(activeIndex + 1).padStart(2, '0')}</strong> / 04
        </span>
        <div
          className='dreamstars-carousel-dots'
          role='group'
          aria-label={t('Choose a scenario')}
        >
          {DREAMSTARS_SCENES.map((scene, index) => (
            <button
              key={scene.id}
              type='button'
              aria-label={t('Show scenario {{number}}: {{title}}', {
                number: index + 1,
                title: t(scene.title),
              })}
              aria-current={index === activeIndex ? 'true' : undefined}
              data-active={index === activeIndex}
              onClick={() => selectScene(index)}
            />
          ))}
        </div>
        <div className='dreamstars-carousel-actions'>
          <button
            type='button'
            aria-label={userPaused ? t('Resume carousel') : t('Pause carousel')}
            aria-pressed={userPaused}
            onClick={() => setUserPaused((value) => !value)}
          >
            {userPaused ? (
              <Play aria-hidden='true' />
            ) : (
              <Pause aria-hidden='true' />
            )}
          </button>
          <button
            type='button'
            aria-label={t('Previous scenario')}
            onClick={() => selectScene(activeIndex - 1)}
          >
            <ChevronLeft aria-hidden='true' />
          </button>
          <button
            type='button'
            aria-label={t('Next scenario')}
            onClick={() => selectScene(activeIndex + 1)}
          >
            <ChevronRight aria-hidden='true' />
          </button>
        </div>
      </div>
    </div>
  )
}
