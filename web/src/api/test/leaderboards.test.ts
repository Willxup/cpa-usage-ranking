import { afterEach, describe, expect, it, vi } from 'vitest'
import { formatLocalizedError } from '../../i18n'
import { fetchLeaderboard } from '../leaderboards'

const signal = () => new AbortController().signal
const response = (body: string, status = 200) => new Response(body, { status, headers: { 'Content-Type': 'application/json' } })
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

describe('leaderboard request errors', () => {
  it.each(['{"entries":', 'private-proxy-detail'])('hides malformed JSON diagnostics: %s', async (body) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(body)))
    await expect(fetchLeaderboard('usage', 'today', signal())).rejects.toThrow('Invalid leaderboard response. Please retry shortly.')
  })

  it('hides transport diagnostics and supports localized re-formatting', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('private-network-detail')))
    let caught: unknown
    try {
      await fetchLeaderboard('usage', 'today', signal())
    } catch (error) {
      caught = error
    }
    expect(caught).toBeInstanceOf(Error)
    expect((caught as Error).message).toBe('Unable to reach server. Check your connection and retry.')
    expect(formatLocalizedError(caught, (m) => m.leaderboardErrors.generic, 'zh')).toBe('无法加载榜单，请检查连接后重试。')
  })

  it('preserves the service-unavailable message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response('{}', 503)))
    await expect(fetchLeaderboard('usage', 'today', signal())).rejects.toThrow('Leaderboard cache is not ready yet. Please retry shortly.')
  })

  it('preserves caller cancellation without starting a request', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    controller.abort('selection changed')
    await expect(fetchLeaderboard('usage', 'today', controller.signal)).rejects.toBe('selection changed')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('retains the timeout message when the response body is interrupted', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn((_input, init: RequestInit) => Promise.resolve({
      ok: true,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: () => new Promise((_resolve, reject) => {
        init.signal!.addEventListener('abort', () => reject(new DOMException('body interrupted', 'AbortError')), { once: true })
      }),
    })))
    const assertion = expect(fetchLeaderboard('usage', 'today', signal())).rejects.toThrow('Leaderboard request timed out. Please retry.')
    await vi.advanceTimersByTimeAsync(8_000)
    await assertion
  })
})
