import { describe, expect, it } from 'vitest'
import { formatBeijingTime } from '../formatTime'

describe('formatBeijingTime', () => {
  it.each([
    ['2026-11-15T15:59:59Z', '2026-11-15 23:59:59'],
    ['2026-11-15T16:00:00Z', '2026-11-16 00:00:00'],
    ['2026-11-30T16:00:00Z', '2026-12-01 00:00:00'],
    ['2026-12-31T16:00:00Z', '2027-01-01 00:00:00'],
    ['2026-12-31T11:00:00-05:00', '2027-01-01 00:00:00'],
  ])('converts %s to Shanghai calendar time', (instant, expected) => {
    expect(formatBeijingTime(instant)).toBe(expected)
  })
})
