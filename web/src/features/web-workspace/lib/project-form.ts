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

// The default export is the same zod object as the named `z` export; it is
// used here because the test runner's CJS interop does not expose `z`.
import z from 'zod'

import {
  isValidProjectNameSegment,
  PROJECT_NAME_SEGMENT_MAX_LENGTH,
} from './project-name'

export const projectNameFormSchema = z.object({
  userName: z.string().refine(isValidProjectNameSegment, {
    message: `User name must be 1-${PROJECT_NAME_SEGMENT_MAX_LENGTH} letters, numbers, or underscores.`,
  }),
  projectName: z.string().refine(isValidProjectNameSegment, {
    message: `Project name must be 1-${PROJECT_NAME_SEGMENT_MAX_LENGTH} letters, numbers, or underscores.`,
  }),
})

export type ProjectNameFormValues = z.infer<typeof projectNameFormSchema>

/** Kept as an alias so existing rename imports remain explicit. */
export const renameProjectFormSchema = projectNameFormSchema
export type RenameProjectFormValues = ProjectNameFormValues
