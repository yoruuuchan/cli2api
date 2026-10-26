import assert from 'node:assert/strict'
import test from 'node:test'
import { expiryDayKey, formatExpiryDate, localDayStart, quotaExpirySummary } from './quotaExpiry.ts'

// Run with Node's built-in TypeScript stripping: node --test src/lib/quotaExpiry.test.mjs
process.env.TZ = 'UTC'
const now = Date.parse('2026-10-24T12:00:00Z')
const at = (value) => Date.parse(value) / 1000
const pack = (remain, date) => ({ remain, ends_at: at(date), unit: 'credits' })

function inTimezone(zone, run) {
  const previous = process.env.TZ
  process.env.TZ = zone
  try {
    run()
  } finally {
    process.env.TZ = previous
  }
}

test('the soonest valid day sums every remaining package on that local day', () => {
  const summary = quotaExpirySummary({ packages: [
    pack(50, '2026-10-26T00:00:00Z'),
    pack(20, '2026-10-25T09:00:00Z'),
    pack(30, '2026-10-25T23:59:59Z'),
  ] }, now)
  assert.equal(summary.soonest, at('2026-10-25T09:00:00Z'))
  assert.equal(summary.soonestRemain, 50)
  assert.equal(summary.daysLeft, 1)
  assert.equal(summary.remain, 100)
  assert.deepEqual(summary.packages.map((entry) => entry.index), [1, 2, 0])
})

test('packages expired earlier today or exactly now are not upcoming credits', () => {
  const summary = quotaExpirySummary({ packages: [
    pack(100, '2026-10-24T09:00:00Z'),
    pack(50, '2026-10-24T12:00:00Z'),
    pack(25, '2026-10-24T18:00:00Z'),
  ] }, now)
  assert.equal(summary.expiredRemain, 150)
  assert.equal(summary.remain, 25)
  assert.equal(summary.soonestRemain, 25)
  assert.equal(summary.daysLeft, 0)
  assert.equal(summary.packages.length, 1)
})

test('depleted, negative, missing, and non-finite balances cannot select an expiry', () => {
  const summary = quotaExpirySummary({ packages: [
    ...[0, -1, undefined, null, NaN, Infinity].map((remain) => pack(remain, '2026-10-24T13:00:00Z')),
    pack(3, '2026-10-26T00:00:00Z'),
  ] }, now)
  assert.equal(summary.soonest, at('2026-10-26T00:00:00Z'))
  assert.equal(summary.remain, 3)
  assert.equal(summary.packages.length, 1)
})

test('missing and invalid dates stay explicitly unknown, not parsed from provider wall clocks', () => {
  const summary = quotaExpirySummary({ packages: [
    ...[undefined, 0, -1, NaN, Infinity, 1e14].map((ends_at) => ({ remain: 10, ends_at, end_time: '2026-10-25 23:59:59' })),
    pack(5, '2026-10-26T00:00:00Z'),
  ] }, now)
  assert.equal(summary.unknownPacks, 6)
  assert.equal(summary.packages.length, 7)
  assert.equal(summary.remain, 65)
  assert.equal(summary.soonestRemain, 5)
  assert.equal(summary.packages[0].ends_at, summary.soonest)
  assert.ok(summary.packages.slice(1).every((entry) => entry.ends_at === 0))
})

test('an unknown-only balance has no manufactured expiry or day count', () => {
  const summary = quotaExpirySummary({ packages: [{ remain: 100 }] }, now)
  assert.equal(summary.unknownPacks, 1)
  assert.equal(summary.soonest, 0)
  assert.equal(summary.soonestRemain, 0)
  assert.equal(summary.daysLeft, -1)
})

test('aggregate expiry, quota resets, and credentials never fabricate package dates', () => {
  const summary = quotaExpirySummary({
    expires_at: at('2026-10-25T00:00:00Z'),
    expiring_remain: 100,
    remaining: 100,
    windows: [{ id: 'daily', reset_at: '2026-10-25T00:00:00Z' }],
    token_expires_at: at('2026-10-25T00:00:00Z'),
  }, now)
  assert.equal(summary.soonest, 0)
  assert.equal(summary.daysLeft, -1)
  assert.deepEqual(summary.packages, [])
})

test('all expired or depleted packages leave no valid upcoming batch', () => {
  const summary = quotaExpirySummary({ packages: [
    pack(20, '2026-10-23T23:59:59Z'),
    pack(0, '2026-10-25T23:59:59Z'),
  ] }, now)
  assert.equal(summary.expiredRemain, 20)
  assert.equal(summary.soonest, 0)
  assert.equal(summary.soonestRemain, 0)
  assert.equal(summary.daysLeft, -1)
  assert.deepEqual(summary.packages, [])
})

test('an already expired package cannot re-enter the same-day total beside a future one', () => {
  const summary = quotaExpirySummary({ packages: [
    pack(90, '2026-10-24T00:00:00Z'),
    pack(10, '2026-10-24T12:00:01Z'),
    { remain: 500 },
  ] }, now)
  assert.equal(summary.soonestRemain, 10)
  assert.equal(summary.expiredRemain, 90)
  assert.equal(summary.unknownPacks, 1)
})

test('absolute seconds are grouped and displayed in browser-local time, not end_time', () => {
  inTimezone('Asia/Tokyo', () => {
    const summary = quotaExpirySummary({ packages: [
      { ...pack(10, '2026-10-24T23:59:59+08:00'), end_time: '2026-10-24 23:59:59' },
      pack(20, '2026-10-25T10:00:00+09:00'),
    ] }, now)
    assert.equal(formatExpiryDate(summary.soonest, true), '2026/10/25')
    assert.equal(expiryDayKey(summary.soonest * 1000), '2026-10-25')
    assert.equal(summary.soonestRemain, 30)
    assert.equal(summary.daysLeft, 1)
    assert.equal(localDayStart(summary.soonest * 1000), Date.parse('2026-10-25T00:00:00+09:00'))
  })
})

test('calendar days survive 23-hour spring and 25-hour autumn DST days', () => {
  inTimezone('America/New_York', () => {
    for (const [current, end] of [
      ['2026-03-08T00:30:00-05:00', '2026-03-09T00:01:00-04:00'],
      ['2026-11-01T00:30:00-04:00', '2026-11-02T00:01:00-05:00'],
    ]) {
      const summary = quotaExpirySummary({ packages: [pack(10, end)] }, Date.parse(current))
      assert.equal(summary.daysLeft, 1)
    }
  })
})

test('advancing a cached snapshot past its expiry selects the next valid package', () => {
  const quota = { packages: [
    pack(10, '2026-10-24T12:00:01Z'),
    pack(20, '2026-10-25T12:00:00Z'),
  ] }
  assert.equal(quotaExpirySummary(quota, now).soonestRemain, 10)
  const next = quotaExpirySummary(quota, now + 1000)
  assert.equal(next.soonestRemain, 20)
  assert.equal(next.daysLeft, 1)
  assert.equal(next.expiredRemain, 10)
})

test('all 19 real packages remain available for details while only live balances drive the summary', () => {
  const quota = { packages: [
    { remain: 0 },
    ...Array.from({ length: 16 }, () => pack(0, '2026-10-23T12:00:00Z')),
    pack(40, '2026-10-24T09:00:00Z'),
    pack(120, '2026-10-25T12:00:00Z'),
  ] }
  const summary = quotaExpirySummary(quota, now)
  assert.equal(summary.details.length, 19)
  assert.equal(summary.details[0].ends_at, 0)
  assert.deepEqual(summary.details.map((entry) => entry.remain), quota.packages.map((entry) => entry.remain))
  assert.equal(summary.details[17].ends_at, at('2026-10-24T09:00:00Z'))
  assert.equal(summary.packages.length, 1)
  assert.equal(summary.soonest, at('2026-10-25T12:00:00Z'))
  assert.equal(summary.soonestRemain, 120)
  assert.equal(summary.remain, 120)
  assert.equal(summary.expiredRemain, 40)
  assert.equal(summary.unknownPacks, 0)
})

test('source quota packages remain untouched after sorting and normalizing unknown dates', () => {
  const quota = { packages: [
    { remain: 50, ends_at: -1 },
    pack(10, '2026-10-25T12:00:00Z'),
  ] }
  const before = structuredClone(quota)
  quotaExpirySummary(quota, now)
  assert.deepEqual(quota, before)
  assert.deepEqual(quotaExpirySummary(undefined, now).packages, [])
})
