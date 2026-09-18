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

/** Matches the server rule: 1-255 characters after trimming. */
export const renameProjectFormSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'Project name is required.')
    .max(255, 'Project name must be 255 characters or fewer.'),
})

export type RenameProjectFormValues = z.infer<typeof renameProjectFormSchema>
