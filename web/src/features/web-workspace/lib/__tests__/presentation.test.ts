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
import { describe, expect, it } from 'vitest'

import {
  CHATGPT_PRESENTATION_PROFILE,
  computePresentation,
  findPresentationProfile,
  remoteScreenSizeForFrame,
  toRemotePoint,
} from '../presentation'

const screen = { width: 1280, height: 720 }
const frame = { width: 1788, height: 940 }

describe('findPresentationProfile', () => {
  it('matches the calibrated provider case insensitively', () => {
    expect(findPresentationProfile('ChatGPT')).toBe(
      CHATGPT_PRESENTATION_PROFILE
    )
    expect(findPresentationProfile(' chatgpt ')).toBe(
      CHATGPT_PRESENTATION_PROFILE
    )
  })

  it('returns null for unknown or empty providers', () => {
    expect(findPresentationProfile('claude')).toBeNull()
    expect(findPresentationProfile(undefined)).toBeNull()
  })
})

describe('computePresentation', () => {
  it('fails closed without a profile, framebuffer size or frame size', () => {
    expect(computePresentation({ provider: 'claude', screen, frame })).toEqual({
      status: 'unavailable',
      reason: 'profile-missing',
    })
    expect(
      computePresentation({ provider: 'chatgpt', screen: null, frame })
    ).toEqual({ status: 'unavailable', reason: 'screen-unknown' })
    expect(
      computePresentation({ provider: 'chatgpt', screen, frame: null })
    ).toEqual({ status: 'unavailable', reason: 'frame-unknown' })
    expect(
      computePresentation({
        provider: 'chatgpt',
        screen: { width: 0, height: 0 },
        frame,
      })
    ).toEqual({ status: 'unavailable', reason: 'screen-unknown' })
  })

  it('keeps the native rail outside the presented region', () => {
    const result = computePresentation({ provider: 'chatgpt', screen, frame })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    expect(result.transform.crop.x).toBe(CHATGPT_PRESENTATION_PROFILE.cropLeft)
    expect(result.transform.crop.width).toBeLessThanOrEqual(
      screen.width - CHATGPT_PRESENTATION_PROFILE.cropLeft
    )
    expect(result.transform.crop.y).toBeGreaterThanOrEqual(
      CHATGPT_PRESENTATION_PROFILE.cropTop
    )
    expect(
      result.transform.crop.y + result.transform.crop.height
    ).toBeLessThanOrEqual(screen.height)
  })

  it('fills the frame without dead space and stays inside the framebuffer', () => {
    const result = computePresentation({ provider: 'chatgpt', screen, frame })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    const { transform } = result
    expect(transform.stageWidth).toBeGreaterThanOrEqual(frame.width)
    expect(transform.stageHeight).toBeGreaterThanOrEqual(frame.height)
    expect(transform.offsetX).toBeLessThanOrEqual(0)
    expect(transform.offsetY).toBeLessThanOrEqual(0)
    expect(transform.offsetX + transform.stageWidth).toBeGreaterThanOrEqual(
      frame.width
    )
    expect(transform.offsetY + transform.stageHeight).toBeGreaterThanOrEqual(
      frame.height
    )
  })

  it('centres the vertical crop inside the visible region below the top chrome', () => {
    const result = computePresentation({ provider: 'chatgpt', screen, frame })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    const { transform } = result
    const bottom = transform.crop.y + transform.crop.height
    const topMargin = transform.crop.y - CHATGPT_PRESENTATION_PROFILE.cropTop
    const bottomMargin = screen.height - bottom
    expect(Math.abs(topMargin - bottomMargin)).toBeLessThanOrEqual(1)
  })

  it('recomputes the crop when the frame changes', () => {
    const wide = computePresentation({ provider: 'chatgpt', screen, frame })
    const narrow = computePresentation({
      provider: 'chatgpt',
      screen,
      frame: { width: 900, height: 640 },
    })
    expect(wide.status).toBe('ready')
    expect(narrow.status).toBe('ready')
    if (wide.status !== 'ready' || narrow.status !== 'ready') return
    expect(narrow.transform.scale).toBeLessThan(wide.transform.scale)
    expect(narrow.transform.offsetX).not.toBe(wide.transform.offsetX)
  })

  it('centres a narrow crop between the rail and the right edge', () => {
    const result = computePresentation({
      provider: 'chatgpt',
      screen,
      frame: { width: 900, height: 640 },
    })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    const { crop } = result.transform
    const visibleWidth = screen.width - CHATGPT_PRESENTATION_PROFILE.cropLeft
    expect(crop.x + crop.width).toBeLessThanOrEqual(screen.width)
    const leftMargin = crop.x - CHATGPT_PRESENTATION_PROFILE.cropLeft
    const rightMargin = visibleWidth - leftMargin - crop.width
    expect(Math.abs(leftMargin - rightMargin)).toBeLessThanOrEqual(1)
  })
  it('caps an oversized profile so the page cannot be cropped away', () => {
    const result = computePresentation({
      provider: 'chatgpt',
      screen: { width: 600, height: 400 },
      frame,
    })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    expect(result.transform.crop.x).toBeLessThanOrEqual(
      600 - CHATGPT_PRESENTATION_PROFILE.minVisibleWidth
    )
  })
})

describe('remoteScreenSizeForFrame', () => {
  it('proposes the frame height and adds the cropped rail to the width', () => {
    expect(
      remoteScreenSizeForFrame(
        { width: 1920, height: 900 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 2180, height: 952 })
    expect(
      remoteScreenSizeForFrame(
        { width: 1280, height: 720 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 1540, height: 772 })
  })

  it('keeps the height inside 720p..1440p', () => {
    expect(
      remoteScreenSizeForFrame(
        { width: 3840, height: 2160 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 2728, height: 1440 })
    expect(
      remoteScreenSizeForFrame(
        { width: 300, height: 200 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 1262, height: 720 })
  })

  it('keeps the width inside 640..3840 and preserves wide aspect ratios', () => {
    expect(
      remoteScreenSizeForFrame(
        { width: 600, height: 1600 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 781, height: 1440 })
    expect(
      remoteScreenSizeForFrame(
        { width: 5000, height: 800 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toEqual({ width: 3840, height: 625 })
  })

  it('scales proportionally into the cloud screen budget', () => {
    const bounds = { maxWidth: 2048, maxHeight: 900 }
    expect(
      remoteScreenSizeForFrame(
        { width: 1920, height: 1080 },
        CHATGPT_PRESENTATION_PROFILE,
        bounds
      )
    ).toEqual({ width: 1768, height: 900 })
    expect(
      remoteScreenSizeForFrame(
        { width: 2560, height: 1080 },
        CHATGPT_PRESENTATION_PROFILE,
        bounds
      )
    ).toEqual({ width: 2048, height: 806 })
    expect(
      remoteScreenSizeForFrame(
        { width: 3840, height: 1080 },
        CHATGPT_PRESENTATION_PROFILE,
        bounds
      )
    ).toEqual({ width: 2048, height: 555 })
  })

  it('returns null when the frame cannot be measured', () => {
    expect(
      remoteScreenSizeForFrame(null, CHATGPT_PRESENTATION_PROFILE)
    ).toBeNull()
    expect(
      remoteScreenSizeForFrame(
        { width: 0, height: 720 },
        CHATGPT_PRESENTATION_PROFILE
      )
    ).toBeNull()
  })
})
describe('toRemotePoint', () => {
  it('maps frame coordinates through the active crop, offset and scale', () => {
    const result = computePresentation({ provider: 'chatgpt', screen, frame })
    expect(result.status).toBe('ready')
    if (result.status !== 'ready') return
    const { transform } = result
    expect(toRemotePoint(transform, { x: 0, y: 0 })).toEqual({
      x: -transform.offsetX / transform.scale,
      y: -transform.offsetY / transform.scale,
    })
    const frameCentre = { x: frame.width * 0.5, y: frame.height * 0.5 }
    expect(toRemotePoint(transform, frameCentre)).toEqual({
      x: (frameCentre.x - transform.offsetX) / transform.scale,
      y: (frameCentre.y - transform.offsetY) / transform.scale,
    })
  })
})
