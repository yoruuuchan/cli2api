import { useEffect, useState } from 'react'
import type { AccountQuotaPackage } from '@/api/types'
import { quotaExpirySummary } from '@/lib/quotaExpiry'

// One clock for the card/overview calculation, refreshed at an expiry or local
// midnight. It updates cached data without a render-time clock read or request.
export function useQuotaExpiryClock(packages?: AccountQuotaPackage[]) {
  const [now, setNow] = useState(Date.now)

  useEffect(() => {
    const details = quotaExpirySummary({ packages }, now).details
    const soonest = details.reduce((next, pack) => (
      pack.ends_at * 1000 > now && (!next || pack.ends_at < next) ? pack.ends_at : next
    ), 0)
    if (!soonest) return
    const tomorrow = new Date(now)
    tomorrow.setHours(24, 0, 0, 0)
    const next = Math.min(soonest * 1000, tomorrow.getTime())
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, next - Date.now()))
    const onVisible = () => {
      if (!document.hidden) setNow(Date.now())
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [packages, now])

  return now
}
