import type { AccountQuota, AccountQuotaPackage } from '../api/types'

const DAY_MS = 24 * 60 * 60 * 1000

export type QuotaPackageDetail = AccountQuotaPackage & {
  index: number
  ends_at: number // Unix seconds; 0 means unknown, never inferred from end_time.
}

export type LiveQuotaPackage = QuotaPackageDetail & { remain: number }

export type QuotaExpirySummary = {
  details: QuotaPackageDetail[]
  packages: LiveQuotaPackage[]
  remain: number
  expiredRemain: number
  unknownPacks: number
  soonest: number
  soonestRemain: number
  daysLeft: number
}

export function localDayStart(timestamp: number) {
  const date = new Date(timestamp)
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
}

export function expiryDayKey(timestamp: number) {
  const date = new Date(timestamp)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

// Keep the console's numeric date style and browser-local timezone. ends_at is
// an absolute Unix second; end_time is a provider wall clock, not a parse fallback.
export function formatExpiryDate(endsAt: number, includeYear = false) {
  const date = new Date(endsAt * 1000)
  const day = `${date.getMonth() + 1}/${date.getDate()}`
  return includeYear ? `${date.getFullYear()}/${day}` : day
}

function localCalendarDay(timestamp: number) {
  const date = new Date(timestamp)
  // Compare calendar dates, not elapsed 24-hour periods across DST changes.
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / DAY_MS
}

export function quotaExpirySummary(quota: AccountQuota | undefined, now = Date.now()): QuotaExpirySummary {
  const details: QuotaPackageDetail[] = []
  const packages: LiveQuotaPackage[] = []
  let remain = 0
  let expiredRemain = 0
  let unknownPacks = 0

  for (const [index, pack] of (quota?.packages ?? []).entries()) {
    const rawExpiry = pack.ends_at ?? 0
    const endsAt = Number.isFinite(rawExpiry) && rawExpiry > 0 && Number.isFinite(new Date(rawExpiry * 1000).getTime())
      ? rawExpiry
      : 0
    const detail = { ...pack, index, ends_at: endsAt }
    // Preserve every real package for disclosure, including exhausted and
    // expired ones; only the upcoming summary filters them out.
    details.push(detail)
    const amount = pack.remain
    if (amount == null || !Number.isFinite(amount) || amount <= 0) continue

    // Expiry is an instant, even when it falls earlier on the current local day.
    if (endsAt && endsAt * 1000 <= now) {
      expiredRemain += amount
      continue
    }
    packages.push({ ...detail, remain: amount })
    remain += amount
    if (!endsAt) unknownPacks += 1
  }

  packages.sort((a, b) => {
    if (a.ends_at && b.ends_at) return a.ends_at - b.ends_at || a.index - b.index
    if (a.ends_at) return -1
    if (b.ends_at) return 1
    return a.index - b.index
  })
  const soonest = packages[0]?.ends_at ?? 0
  const soonestDay = soonest ? expiryDayKey(soonest * 1000) : ''
  const soonestRemain = packages.reduce((sum, pack) => (
    pack.ends_at && expiryDayKey(pack.ends_at * 1000) === soonestDay ? sum + pack.remain : sum
  ), 0)
  const daysLeft = soonest ? Math.max(0, localCalendarDay(soonest * 1000) - localCalendarDay(now)) : -1

  return { details, packages, remain, expiredRemain, unknownPacks, soonest, soonestRemain, daysLeft }
}
