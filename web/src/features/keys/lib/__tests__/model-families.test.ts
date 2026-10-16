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
import { describe, expect, test } from 'vitest'

import type { UserModelFamilies } from '../../types'
import {
  familyLimitFromSelection,
  familyLimitNames,
  initialFamilySelection,
  sameFamilyLimit,
  selectableFamilyIds,
  UNCLASSIFIED_FAMILY_ID,
} from '../model-families'

const available: UserModelFamilies = {
  families: [
    { id: 1, key: 'claude', name: 'Claude', description: '', models: [] },
    { id: 2, key: 'gpt', name: 'GPT', description: '', models: ['gpt-5'] },
    {
      id: 3,
      key: 'qwen',
      name: 'Qwen',
      description: '',
      models: ['qwen-plus'],
    },
  ],
  unclassified_models: ['custom-model'],
}
const everythingClassified: UserModelFamilies = {
  ...available,
  unclassified_models: [],
}

describe('selectableFamilyIds', () => {
  test('offers every family and the models without a family when such models exist', () => {
    expect(selectableFamilyIds(available)).toEqual([
      1,
      2,
      3,
      UNCLASSIFIED_FAMILY_ID,
    ])
  })

  test('leaves out the models without a family when every model belongs to one', () => {
    expect(selectableFamilyIds(everythingClassified)).toEqual([1, 2, 3])
  })

  test('keeps the models without a family for a key that already includes them', () => {
    expect(selectableFamilyIds(everythingClassified, [0, 2])).toEqual([
      1, 2, 3, 0,
    ])
  })
})

describe('initialFamilySelection', () => {
  test('selects everything for a key without a limit', () => {
    expect([...initialFamilySelection(available, [])].sort()).toEqual([
      0, 1, 2, 3,
    ])
  })

  test('keeps only the stored families that still exist', () => {
    expect([...initialFamilySelection(available, [2, 99])]).toEqual([2])
  })
})

describe('familyLimitFromSelection', () => {
  test('stores no limit when everything is selected, so later models stay available', () => {
    expect(
      familyLimitFromSelection(available, [], new Set([0, 1, 2, 3]))
    ).toEqual([])
  })

  test('stores the sorted ids of a partial selection', () => {
    expect(familyLimitFromSelection(available, [], new Set([3, 0, 1]))).toEqual(
      [0, 1, 3]
    )
  })

  test('refuses an empty selection instead of turning it into no limit', () => {
    expect(() => familyLimitFromSelection(available, [], new Set())).toThrow()
  })

  test('stores no limit when there is nothing to choose from', () => {
    const none: UserModelFamilies = { families: [], unclassified_models: [] }
    expect(familyLimitFromSelection(none, [], new Set())).toEqual([])
  })
})

describe('sameFamilyLimit', () => {
  test('compares limits as sets', () => {
    expect(sameFamilyLimit([1, 2], [2, 1])).toBe(true)
    expect(sameFamilyLimit([1], [1, 2])).toBe(false)
    expect(sameFamilyLimit([], [])).toBe(true)
  })
})

describe('familyLimitNames', () => {
  test('names the limited families in list order with the other models last', () => {
    expect(familyLimitNames([3, 0, 1], available, 'Other models')).toEqual([
      'Claude',
      'Qwen',
      'Other models',
    ])
  })

  test('skips families that no longer exist', () => {
    expect(familyLimitNames([99], available, 'Other models')).toEqual([])
  })

  test('has no names before the families are loaded', () => {
    expect(familyLimitNames([1], undefined, 'Other models')).toEqual([])
  })
})
