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
import { useQuery } from '@tanstack/react-query'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getUserModelFamilies } from '../api'
import type { UserModelFamilies } from '../types'

export const userModelFamiliesQueryKey = ['user-model-families'] as const

const NO_FAMILIES: UserModelFamilies = {
  families: [],
  unclassified_models: [],
}

/** The model families with the models the current user can use in each. */
export function useUserModelFamilies(enabled = true) {
  return useQuery({
    queryKey: userModelFamiliesQueryKey,
    queryFn: async () =>
      requireServerSuccess(await getUserModelFamilies()).data ?? NO_FAMILIES,
    enabled,
    staleTime: 0,
    // The list shows family names as a convenience; a failure there should
    // not raise an error toast for every row.
    meta: { errorToast: false },
  })
}
