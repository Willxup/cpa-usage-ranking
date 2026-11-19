import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchLeaderboard } from '../leaderboards'

const details = { total_tokens: '9223372036854775807', request_count: '9007199254740993', success_rate: '75.00', cache_read_rate: '25.13', ttft_mean_ms: '10.33', latency_mean_ms: '200.50' }
function response(extra: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T12:00:00Z', entries: [{ rank: 1, client_id: '1', avatar_id: '1', display_name: '真实数据', metric: '51.16', ...extra }] }), { headers: { 'Content-Type': 'application/json' } })))
}
afterEach(() => vi.unstubAllGlobals())

describe('overall details contract', () => {
  it('preserves int64 strings and all six detail values', async () => {
    response({ details })
    expect((await fetchLeaderboard('overall', 'today', new AbortController().signal)).entries[0].details).toEqual(details)
  })
  it('distinguishes null measurements from measured zero', async () => {
    const values = { ...details, cache_read_rate: null, ttft_mean_ms: null, latency_mean_ms: '0.00' }
    response({ details: values })
    expect((await fetchLeaderboard('overall', 'today', new AbortController().signal)).entries[0].details).toEqual(values)
  })
  it('accepts an older response without inventing detail values', async () => {
    response({})
    expect((await fetchLeaderboard('overall', 'today', new AbortController().signal)).entries[0].details).toBeUndefined()
  })
  it.each([
    { total_tokens: 12 }, { request_count: '9223372036854775808' }, { success_rate: '101.00' },
    { cache_read_rate: '-1.00' }, { ttft_mean_ms: 'Infinity' }, { latency_mean_ms: '1e3' }, { latency_mean_ms: undefined },
  ])('rejects malformed details %j', async (invalid) => {
    response({ details: { ...details, ...invalid } })
    await expect(fetchLeaderboard('overall', 'today', new AbortController().signal)).rejects.toThrow('Invalid leaderboard response. Please retry shortly.')
  })
})
