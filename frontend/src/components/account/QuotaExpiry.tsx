import { Accordion } from '@heroui/react'
import type { AccountQuota } from '@/api/types'
import { formatQuotaAmount } from '@/lib/account'
import { formatExpiryDate, quotaExpirySummary, type QuotaPackageDetail } from '@/lib/quotaExpiry'
import { useQuotaExpiryClock } from '@/hooks/useQuotaExpiryRefresh'

type Translate = (key: string, vars?: Record<string, string | number>) => string

export function QuotaExpiry({ quota, t }: { quota?: AccountQuota; t: Translate }) {
  const now = useQuotaExpiryClock(quota?.packages)
  const expiry = quotaExpirySummary(quota, now)
  const unit = expiry.packages[0]?.unit || quota?.unit || 'credits'
  const tone = expiry.daysLeft <= 2 ? 'text-danger' : expiry.daysLeft <= 7 ? 'text-warning' : 'text-muted'
  const unknown = expiry.unknownPacks > 0 || !quota?.packages?.length
  const summary = (
    <span className="block min-w-0 flex-1 space-y-0.5 text-left text-caption leading-5">
      {expiry.soonest ? (
        <>
          <span className="flex flex-wrap items-baseline gap-x-2">
            <span className="text-muted">{t('expiryDistColSoonest')}</span>
            <time className="mono text-foreground" dateTime={new Date(expiry.soonest * 1000).toISOString()}>{formatExpiryDate(expiry.soonest, true)}</time>
            <span className={`mono ${tone}`}>{t('expiryDistDays', { n: expiry.daysLeft })}</span>
          </span>
          <span className="block text-muted">
            {t('expiryDistColBatch')} <span className="mono text-foreground/70">{formatQuotaAmount(expiry.soonestRemain)} {unit}</span>
          </span>
        </>
      ) : <span className="text-muted">{t(unknown ? 'quotaExpiryUnknown' : 'quotaExpiryNoActive')}</span>}
      {expiry.soonest && expiry.unknownPacks > 0 ? (
        <span className="block text-warning">{t('quotaExpiryPartialUnknown', { n: expiry.unknownPacks })}</span>
      ) : null}
    </span>
  )
  // Every real package is visible without interaction. A single package is
  // shown inline because a tooltip is not reachable by touch and not reliably
  // opened by focus; multi-package cards keep the keyboard-accessible accordion.
  const packageRow = (pack: QuotaPackageDetail, key: string) => {
    const depleted = pack.remain != null && Number.isFinite(pack.remain) && pack.remain <= 0
    const expired = pack.ends_at > 0 && pack.ends_at * 1000 <= now
    return (
      <li key={key} className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-3 text-caption leading-5">
        <span className="flex min-w-0 flex-wrap gap-x-2">
          <span className="text-muted">{t('quotaExpiryPackage', { n: pack.index + 1 })}</span>
          <span className="mono text-foreground">{formatQuotaAmount(pack.remain)} {pack.unit || quota?.unit || 'credits'}</span>
          {depleted ? <span className="text-muted">{t('quotaExpiryDepleted')}</span> : null}
          {expired ? <span className="text-danger">{t('expiryDistExpired')}</span> : null}
        </span>
        {pack.ends_at ? (
          <time className="mono text-muted" dateTime={new Date(pack.ends_at * 1000).toISOString()} title={new Date(pack.ends_at * 1000).toLocaleString(undefined, { timeZoneName: 'short' })}>
            {formatExpiryDate(pack.ends_at, true)}
          </time>
        ) : <span className="text-warning">{t('quotaExpiryUnknown')}</span>}
      </li>
    )
  }

  if (expiry.details.length <= 1) {
    if (!expiry.details.length) {
      return <div className="w-full pt-1">{summary}</div>
    }
    return (
      <div className="w-full space-y-1 pt-1">
        {summary}
        <ul className="space-y-1" aria-label={t('quotaExpiryPackages', { n: 1 })}>
          {packageRow(expiry.details[0], 'single')}
        </ul>
        <p className="text-caption leading-5 text-muted">{t('quotaExpiryLocalTime')}</p>
      </div>
    )
  }

  return (
    <Accordion hideSeparator className="w-full">
      <Accordion.Item id="quota-packages">
        <Accordion.Heading>
          <Accordion.Trigger className="items-center gap-2 px-0 py-1 text-left">
            {summary}
            <span className="shrink-0 text-caption font-normal text-muted">{t('quotaExpiryPackages', { n: expiry.details.length })}</span>
            <Accordion.Indicator />
          </Accordion.Trigger>
        </Accordion.Heading>
        <Accordion.Panel>
          <Accordion.Body className="space-y-2 px-0 pt-1 pb-0">
            <ul className="max-h-60 space-y-1.5 overflow-y-auto pr-1" tabIndex={0} aria-label={t('quotaExpiryPackages', { n: expiry.details.length })}>
              {expiry.details.map((pack) => packageRow(pack, String(pack.index)))}
            </ul>
            <p className="text-caption leading-5 text-muted">{t('quotaExpiryLocalTime')}</p>
          </Accordion.Body>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  )
}
