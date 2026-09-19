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
*/import { describe, expect, test } from 'vitest'

import { formatByteSize } from '../bytes'

describe('formatByteSize', () => {
  test('formats the transferred framebuffer size', () => {
    expect(formatByteSize(0)).toBe('0 B')
    expect(formatByteSize(721)).toBe('721 B')
    expect(formatByteSize(86500)).toBe('84.5 KB')
    expect(formatByteSize(2020000)).toBe('1.9 MB')
    expect(formatByteSize(3000000000)).toBe('2.8 GB')
  })

  test('fails closed for values that cannot describe a byte count', () => {
    expect(formatByteSize(-1)).toBe('0 B')
    expect(formatByteSize(Number.NaN)).toBe('0 B')
    expect(formatByteSize(Number.POSITIVE_INFINITY)).toBe('0 B')
  })
})