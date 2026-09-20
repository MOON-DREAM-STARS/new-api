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
  composeProjectName,
  isValidProjectNameSegment,
  splitProjectName,
} from '../project-name'
import { renameProjectFormSchema } from '../project-form'

describe('project name convention', () => {
  it('accepts Unicode letters, digits and underscores in each segment', () => {
    expect(isValidProjectNameSegment('张三')).toBe(true)
    expect(isValidProjectNameSegment('user_01')).toBe(true)
    expect(isValidProjectNameSegment('项目A')).toBe(true)
  })

  it('rejects blank, spaced, hyphenated or oversized segments', () => {
    expect(isValidProjectNameSegment('   ')).toBe(false)
    expect(isValidProjectNameSegment('user name')).toBe(false)
    expect(isValidProjectNameSegment('user-name')).toBe(false)
    expect(isValidProjectNameSegment('a'.repeat(25))).toBe(false)
  })

  it('composes and splits only the single hyphen convention', () => {
    expect(composeProjectName(' 张三 ', ' 项目A ')).toBe('张三-项目A')
    expect(splitProjectName('张三-项目A')).toEqual({
      userName: '张三',
      projectName: '项目A',
    })
    expect(splitProjectName('legacy name')).toBeNull()
    expect(splitProjectName('a-b-c')).toBeNull()
  })

  it('applies the same validation to rename', () => {
    expect(
      renameProjectFormSchema.safeParse({
        userName: '张三',
        projectName: '项目A',
      }).success
    ).toBe(true)
    expect(
      renameProjectFormSchema.safeParse({
        userName: 'user name',
        projectName: '项目A',
      }).success
    ).toBe(false)
  })
})
