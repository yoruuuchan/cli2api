import { useMemo, useState } from 'react'
import { Button, Card, Table } from '@heroui/react'
import type { AccountRow } from '@/lib/account'
import { formatQuotaAmount } from '@/lib/account'
import { expiryDayKey, formatExpiryDate, localDayStart, quotaExpirySummary, type QuotaExpirySummary } from '@/lib/quotaExpiry'
import { useQuotaExpiryClock } from '@/hooks/useQuotaExpiryRefresh'

// All dates and balances come from the existing quota.packages payload. Share
// the card's instant-based expiry filter and browser-local calendar-day math.
type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  accounts: AccountRow[]
  t: Translate
}

// Categorical identity palette — stable by account index across chart and table.
const SERIES = 8
function seriesColor(index: number) {
  return `var(--chart-${(index % SERIES) + 1})`
}

type Segment = { id: string; name: string; color: string; remain: number }
type DayBucket = { day: string; label: string; ts: number; segments: Segment[]; total: number }
type AccountExpiry = QuotaExpirySummary & {
  id: string
  name: string
  color: string
  dailyBurn: number
}

export function ExpiryDistribution({ accounts, t }: Props) {
  const [expanded, setExpanded] = useState(false)
  const packages = useMemo(() => accounts.flatMap((account) => account.quota?.packages ?? []), [accounts])
  const now = useQuotaExpiryClock(packages)
  const todayStart = localDayStart(now)
  const byDay = new Map<string, DayBucket>()
  const rows: AccountExpiry[] = []

  for (const [index, account] of accounts.entries()) {
    const expiry = quotaExpirySummary(account.quota, now)
    const color = seriesColor(index)
    const name = account.name || account.id

    function addAmount(day: string, ts: number, label: string, amount: number) {
      let bucket = byDay.get(day)
      if (!bucket) {
        bucket = { day, label, ts, segments: [], total: 0 }
        byDay.set(day, bucket)
      }
      const existing = bucket.segments.find((segment) => segment.id === account.id)
      if (existing) existing.remain += amount
      else bucket.segments.push({ id: account.id, name, color, remain: amount })
      bucket.total += amount
    }

    // Retain the explicitly labelled expired bucket, including packs that
    // expired earlier today. They never contribute to upcoming balances.
    if (expiry.expiredRemain > 0) addAmount('__expired__', todayStart - 1, '!', expiry.expiredRemain)
    for (const pack of expiry.packages) {
      if (!pack.ends_at) continue // Unknown dates stay visible in the table.
      const ends = pack.ends_at * 1000
      addAmount(expiryDayKey(ends), localDayStart(ends), formatExpiryDate(pack.ends_at), pack.remain)
    }
    rows.push({
      ...expiry,
      id: account.id,
      name,
      color,
      dailyBurn: expiry.soonest ? expiry.soonestRemain / Math.max(expiry.daysLeft, 1) : 0,
    })
  }

  rows.sort((a, b) => {
    if (a.soonest && b.soonest) return a.soonest - b.soonest
    if (a.soonest) return -1
    if (b.soonest) return 1
    return b.remain - a.remain
  })
  const buckets = [...byDay.values()].sort((a, b) => a.ts - b.ts)
  const maxTotal = buckets.reduce((maximum, bucket) => Math.max(maximum, bucket.total), 0)
  const visibleRows = rows.filter((row) => row.packages.length > 0 || row.expiredRemain > 0)
  if (!visibleRows.length) return null
  const shownBuckets = expanded ? buckets : buckets.slice(0, 8)

  return (
    <Card className="overflow-hidden p-0" data-gsap-reveal>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-separator px-5 py-4">
        <div className="min-w-0">
          <h3 className="font-semibold">{t('expiryDistTitle')}</h3>
          <p className="mt-0.5 text-xs text-muted">{t('expiryDistHint')}</p>
        </div>
        {buckets.length > 8 ? (
          <Button size="sm" variant="ghost" onPress={() => setExpanded((value) => !value)}>
            {expanded ? t('expiryDistCollapse') : t('expiryDistExpand', { n: buckets.length })}
          </Button>
        ) : null}
      </div>

      <div className="space-y-3 p-5">
        {/* One bar per local calendar day; each segment identifies an account. */}
        <div className="space-y-1.5">
          {shownBuckets.map((bucket) => {
            const isToday = bucket.ts === todayStart
            const scale = maxTotal > 0 ? (bucket.total / maxTotal) * 100 : 0
            return (
              <div key={bucket.day} className="grid grid-cols-[52px_minmax(0,1fr)_auto] items-center gap-2">
                <span className={`mono text-caption ${bucket.day === '__expired__' ? 'font-semibold text-danger' : isToday ? 'font-semibold text-foreground' : 'text-muted'}`}>
                  {bucket.day === '__expired__' ? t('expiryDistExpired') : bucket.label}
                </span>
                <div
                  className="h-2.5 w-full rounded-full bg-surface-tertiary"
                  role="img"
                  aria-label={`${bucket.day === '__expired__' ? t('expiryDistExpired') : bucket.day} ${t('expiryDistDayTotal', { n: formatQuotaAmount(bucket.total) })}`}
                  title={bucket.day === '__expired__' ? t('expiryDistExpired') : bucket.day}
                >
                  <div className="flex h-full items-stretch overflow-hidden rounded-full" style={{ width: `${scale}%` }}>
                    {bucket.segments.map((segment) => (
                      <span
                        key={segment.id}
                        style={{ width: `${(segment.remain / bucket.total) * 100}%`, backgroundColor: segment.color }}
                        title={`${segment.name} · ${formatQuotaAmount(segment.remain)}`}
                      />
                    ))}
                  </div>
                </div>
                <span className="mono text-caption text-foreground">{formatQuotaAmount(bucket.total)}</span>
              </div>
            )
          })}
        </div>

        <div className="flex flex-wrap gap-x-3 gap-y-1.5 pt-1">
          {rows
            .filter((row) => row.soonest > 0 || row.expiredRemain > 0)
            .map((row) => (
              <span key={row.id} className="inline-flex items-center gap-1.5 text-caption text-muted">
                <i className="size-2 rounded-[3px]" style={{ backgroundColor: row.color }} />
                <span className="max-w-[120px] truncate">{row.name}</span>
              </span>
            ))}
        </div>

        <Table aria-label={t('expiryDistTitle')}>
          <Table.ScrollContainer>
            <Table.Content>
              <Table.Header>
                <Table.Column isRowHeader>{t('expiryDistColAccount')}</Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColSoonest')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColDaysLeft')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColBatch')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColDaily')}</div></Table.Column>
              </Table.Header>
              <Table.Body>
                {visibleRows.map((row) => {
                  const urgent = row.daysLeft >= 0 && row.daysLeft <= 2
                  const soon = row.daysLeft >= 0 && row.daysLeft <= 7
                  const onlyExpired = !row.packages.length && row.expiredRemain > 0
                  return (
                    <Table.Row key={row.id}>
                      <Table.Cell>
                        <span className="inline-flex items-center gap-1.5">
                          <i className="size-2 shrink-0 rounded-[3px]" style={{ backgroundColor: row.color }} />
                          <span className="max-w-[150px] truncate text-foreground">{row.name}</span>
                        </span>
                      </Table.Cell>
                      <Table.Cell>
                        <div className="text-center text-muted">
                          {row.soonest ? <span className="mono">{formatExpiryDate(row.soonest)}</span> : row.unknownPacks > 0 ? t('quotaExpiryUnknown') : '—'}
                          {row.soonest && row.unknownPacks > 0 ? <div className="text-caption text-warning">{t('quotaExpiryPartialUnknown', { n: row.unknownPacks })}</div> : null}
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <div className={`mono text-center ${onlyExpired || urgent ? 'font-semibold text-danger' : soon ? 'text-warning' : 'text-muted'}`}>
                          {row.daysLeft >= 0 ? t('expiryDistDays', { n: row.daysLeft }) : onlyExpired ? t('expiryDistExpired') : '—'}
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <div className="mono text-center text-foreground">{row.soonest ? formatQuotaAmount(row.soonestRemain) : '—'}</div>
                      </Table.Cell>
                      <Table.Cell>
                        <div className="mono text-center text-foreground">{row.soonest ? formatQuotaAmount(row.dailyBurn) : '—'}</div>
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>

        <p className="text-caption text-muted">{t('expiryDistFoot')}</p>
      </div>
    </Card>
  )
}
