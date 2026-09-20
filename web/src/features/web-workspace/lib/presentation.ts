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
/**
 * Remote presentation adapter.
 *
 * The workspace never renders the provider page itself: it streams the real
 * remote browser framebuffer and only decides which part of that framebuffer
 * may be shown. A provider profile describes the native chrome that must stay
 * outside the presented region (for ChatGPT: the native left sidebar, plan,
 * settings and account entries). Everything else is derived from the real
 * framebuffer size reported by the remote renderer and the real size of the
 * workspace frame, so a host resize recomputes the crop instead of reusing a
 * baked-in rectangle.
 *
 * Cropping is presentation only. It is never an authorization boundary: the
 * control plane and the browser guard keep enforcing project ownership.
 */

/** Remote framebuffer size in remote pixels, reported by the renderer. */
export type RemoteScreenSize = {
  width: number
  height: number
}

/** Visible workspace frame size in CSS pixels. */
export type FrameSize = {
  width: number
  height: number
}

/**
 * Native region of a provider page that must not be presented.
 *
 * `cropLeft` and `cropTop` are the provider chrome sizes on the left and top
 * edges of the framebuffer in remote pixels; `minVisibleWidth` is a floor that
 * keeps a mis-measured profile from cropping away the whole page.
 */
export type PresentationProfile = {
  cropLeft: number
  cropTop: number
  minVisibleWidth: number
}

/**
 * Calibrated ChatGPT profile for the runtime screen (1280x720 at the time of
 * calibration): the native sidebar occupies the left 260 remote pixels. The
 * value is a layout constant of the remote page, not of this app, so it must
 * be re-calibrated whenever the provider changes its shell.
 */
export const CHATGPT_PRESENTATION_PROFILE: PresentationProfile = {
  cropLeft: 260,
  cropTop: 52,
  minVisibleWidth: 480,
}

/**
 * Bounds of the remote screen the workspace proposes to the runtime. The agent
 * validates the same range, so an out-of-range proposal is refused instead of
 * silently changing the remote display.
 */
export const REMOTE_SCREEN_MIN_WIDTH = 640
export const REMOTE_SCREEN_MAX_WIDTH = 3840
export const REMOTE_SCREEN_MIN_HEIGHT = 720
export const REMOTE_SCREEN_MAX_HEIGHT = 1440

export type RemoteScreenBounds = {
  maxWidth: number
  maxHeight: number
}

export const DEFAULT_REMOTE_SCREEN_BOUNDS: RemoteScreenBounds = {
  maxWidth: REMOTE_SCREEN_MAX_WIDTH,
  maxHeight: REMOTE_SCREEN_MAX_HEIGHT,
}
export const PRESENTATION_PROFILES: Record<string, PresentationProfile> = {
  chatgpt: CHATGPT_PRESENTATION_PROFILE,
}

/** Region of the remote framebuffer that is presented, in remote pixels. */
export type PresentationCrop = {
  x: number
  y: number
  width: number
  height: number
}

/**
 * Geometry handed to the renderer: the crop keeps the native rail out of the
 * frame, the scale fills the frame without leaving dead space and the offsets
 * position the scaled framebuffer inside the clipping frame.
 */
export type PresentationTransform = {
  crop: PresentationCrop
  scale: number
  offsetX: number
  offsetY: number
  stageWidth: number
  stageHeight: number
}

export type PresentationUnavailableReason =
  | 'profile-missing'
  | 'screen-unknown'
  | 'frame-unknown'
  | 'invalid-geometry'

export type PresentationResult =
  | { status: 'ready'; transform: PresentationTransform }
  | { status: 'unavailable'; reason: PresentationUnavailableReason }

export type PresentationInput = {
  provider: string | null | undefined
  screen: RemoteScreenSize | null | undefined
  frame: FrameSize | null | undefined
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max)
}

function isUsableSize(size: RemoteScreenSize | FrameSize | null | undefined) {
  return Boolean(size && size.width > 0 && size.height > 0)
}

/** Resolves the calibrated profile of a provider, or null when unknown. */
export function findPresentationProfile(
  provider: string | null | undefined
): PresentationProfile | null {
  if (!provider) return null
  return PRESENTATION_PROFILES[provider.trim().toLowerCase()] ?? null
}

/**
 * Computes the presented region and the renderer geometry from the real
 * framebuffer and frame sizes. Fails closed: an unknown provider or an
 * unknown geometry never falls back to the uncropped framebuffer.
 */
export function computePresentation(
  input: PresentationInput
): PresentationResult {
  const profile = findPresentationProfile(input.provider)
  if (!profile) return { status: 'unavailable', reason: 'profile-missing' }
  if (!isUsableSize(input.screen)) {
    return { status: 'unavailable', reason: 'screen-unknown' }
  }
  if (!isUsableSize(input.frame)) {
    return { status: 'unavailable', reason: 'frame-unknown' }
  }

  const screen = input.screen as RemoteScreenSize
  const frame = input.frame as FrameSize
  const maxCropLeft = Math.max(0, screen.width - profile.minVisibleWidth)
  const cropLeft = clamp(Math.round(profile.cropLeft), 0, maxCropLeft)
  const cropTop = clamp(
    Math.round(profile.cropTop),
    0,
    Math.max(0, screen.height - 1)
  )
  const visibleWidth = screen.width - cropLeft
  const visibleHeight = screen.height - cropTop
  if (visibleWidth <= 0 || visibleHeight <= 0) {
    return { status: 'unavailable', reason: 'invalid-geometry' }
  }

  const scale = Math.max(
    frame.width / visibleWidth,
    frame.height / visibleHeight
  )
  if (!Number.isFinite(scale) || scale <= 0) {
    return { status: 'unavailable', reason: 'invalid-geometry' }
  }

  const cropWidth = Math.min(visibleWidth, frame.width / scale)
  const cropHeight = Math.min(visibleHeight, frame.height / scale)
  // The presented region is centred inside the provider-free part of the
  // framebuffer, so the native rail and top chrome stay hidden and a crop that
  // does not fill the visible area leaves equal margins.
  const cropX = cropLeft + Math.round((visibleWidth - cropWidth) / 2)
  const cropY =
    cropTop +
    clamp(
      Math.round((visibleHeight - cropHeight) / 2),
      0,
      Math.max(0, visibleHeight - cropHeight)
    )

  return {
    status: 'ready',
    transform: {
      crop: { x: cropX, y: cropY, width: cropWidth, height: cropHeight },
      scale,
      offsetX: -cropX * scale,
      offsetY: -cropY * scale,
      stageWidth: screen.width * scale,
      stageHeight: screen.height * scale,
    },
  }
}
/**
 * Derives the remote screen size that fits the measured frame.
 *
 * The visible area after the provider rail and top chrome are cropped follows
 * the frame aspect ratio at 1:1 scale. The returned framebuffer height includes
 * the cropped top chrome, while the existing width and height bounds continue
 * to apply to the full framebuffer. Returns null when the frame cannot be
 * measured, which keeps the agent default instead of proposing a guessed size.
 */
export function remoteScreenSizeForFrame(
  frame: FrameSize | null | undefined,
  profile: PresentationProfile,
  bounds?: Partial<RemoteScreenBounds> | null
): RemoteScreenSize | null {
  if (!isUsableSize(frame)) return null
  const measured = frame as FrameSize
  const aspect = measured.width / measured.height
  if (!Number.isFinite(aspect) || aspect <= 0) return null

  const maxWidth = clamp(
    Math.round(bounds?.maxWidth ?? DEFAULT_REMOTE_SCREEN_BOUNDS.maxWidth),
    REMOTE_SCREEN_MIN_WIDTH,
    REMOTE_SCREEN_MAX_WIDTH
  )
  const maxHeight = clamp(
    Math.round(bounds?.maxHeight ?? DEFAULT_REMOTE_SCREEN_BOUNDS.maxHeight),
    REMOTE_SCREEN_MIN_HEIGHT,
    REMOTE_SCREEN_MAX_HEIGHT
  )
  const cropLeft = clamp(
    Math.round(profile.cropLeft),
    0,
    Math.max(0, maxWidth - 1)
  )
  const cropTop = clamp(
    Math.round(profile.cropTop),
    0,
    Math.max(0, maxHeight - 1)
  )
  const maxVisibleWidth = Math.max(profile.minVisibleWidth, maxWidth - cropLeft)
  const maxVisibleHeight = Math.max(1, maxHeight - cropTop)
  const minVisibleHeight = Math.min(
    Math.max(1, REMOTE_SCREEN_MIN_HEIGHT - cropTop),
    maxVisibleHeight
  )

  let visibleHeight = clamp(
    Math.round(measured.height),
    minVisibleHeight,
    maxVisibleHeight
  )
  let visibleWidth = visibleHeight * aspect
  if (visibleWidth > maxVisibleWidth) {
    visibleWidth = maxVisibleWidth
    // Preserve the frame aspect ratio when the width budget is the limiting
    // factor. Very wide frames may therefore fall below 720p, but never below
    // the agent's own 360px minimum framebuffer height.
    visibleHeight = Math.max(Math.max(1, 360 - cropTop), visibleWidth / aspect)
  }

  let height = Math.round(cropTop + visibleHeight)
  if (height > maxHeight) {
    height = maxHeight
    visibleHeight = height - cropTop
    visibleWidth = visibleHeight * aspect
  }

  let width = Math.round(cropLeft + visibleWidth)
  if (width > maxWidth) {
    width = maxWidth
    visibleWidth = Math.max(0, width - cropLeft)
    visibleHeight = Math.max(Math.max(1, 360 - cropTop), visibleWidth / aspect)
    height = Math.round(cropTop + visibleHeight)
  }
  width = clamp(width, REMOTE_SCREEN_MIN_WIDTH, maxWidth)
  height = clamp(height, Math.min(360, maxHeight), maxHeight)
  return { width, height }
}
/**
 * Maps a pointer position inside the frame to remote framebuffer pixels.
 *
 * The renderer forwards pointer and wheel input itself, and its own mapping is
 * rect based (bounding rect divided by the applied scale), so this helper
 * exists to document and unit test the same transform instead of duplicating
 * input handling.
 */
export function toRemotePoint(
  transform: PresentationTransform,
  point: { x: number; y: number }
): { x: number; y: number } {
  return {
    x: (point.x - transform.offsetX) / transform.scale,
    y: (point.y - transform.offsetY) / transform.scale,
  }
}
