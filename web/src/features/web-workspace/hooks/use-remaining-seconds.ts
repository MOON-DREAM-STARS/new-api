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
import { useEffect, useState } from 'react'

import { remainingSeconds } from '../lib/session'

/**
 * Deadline-based countdown in seconds. A refetch that moves the deadline is
 * picked up immediately, and the timer only runs while a deadline exists.
 */
export function useRemainingSeconds(
  deadlineUnixSeconds: number | undefined
): number {
  const [nowMs, setNowMs] = useState(() => Date.now())

  useEffect(() => {
    if (!deadlineUnixSeconds) return undefined
    setNowMs(Date.now())
    const timer = window.setInterval(() => {
      setNowMs(Date.now())
    }, 1000)
    return () => {
      window.clearInterval(timer)
    }
  }, [deadlineUnixSeconds])

  if (!deadlineUnixSeconds) return 0
  return remainingSeconds(deadlineUnixSeconds, nowMs)
}
