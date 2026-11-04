import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkHealth } from '../health'

const response = (data: unknown) => new Response(JSON.stringify(data), { headers: { 'Content-Type': 'application/json' } })
const signal = () => new AbortController().signal

afterEach(() => vi.useRealTimers())

describe('checkHealth', () => {
  it('checks the real same-origin contract without credentials or caching', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ status: 'ok' }))
    vi.stubGlobal('fetch', fetcher)
    await expect(checkHealth(signal())).resolves.toBeUndefined()
    expect(fetcher).toHaveBeenCalledWith('/healthz', expect.objectContaining({
      cache: 'no-store', credentials: 'omit', redirect: 'error', headers: { Accept: 'application/json' },
    }))
  })

  it('reports status failures without exposing a response body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('private implementation detail', { status: 503 })))
    await expect(checkHealth(signal())).rejects.toThrow('服务返回 503，请稍后重试。')
  })

  it.each([null, [], {}, { status: 'not_ready' }, { status: true }])('rejects an invalid success contract: %j', async (data) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(data)))
    await expect(checkHealth(signal())).rejects.toThrow('健康检查响应无效')
  })

  it('rejects HTML fallback pages and malformed JSON', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response('<html>SPA</html>', { headers: { 'Content-Type': 'text/html' } }))
      .mockResolvedValueOnce(new Response('{', { headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetcher)
    await expect(checkHealth(signal())).rejects.toThrow('健康检查响应无效')
    await expect(checkHealth(signal())).rejects.toThrow('健康检查响应无效')
  })

  it('does not expose raw network errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('private host information')))
    await expect(checkHealth(signal())).rejects.toThrow('无法连接服务，请检查网络或稍后重试。')
  })

  it('aborts a stalled request after eight seconds and clears its timer', async () => {
    vi.useFakeTimers()
    let requestSignal: AbortSignal | undefined
    vi.stubGlobal('fetch', vi.fn((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      requestSignal = init.signal!
      requestSignal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
    })))
    const assertion = expect(checkHealth(signal())).rejects.toThrow('连接超时')
    await vi.advanceTimersByTimeAsync(8_000)
    await assertion
    expect(requestSignal?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('honors caller cancellation and does not start an already-cancelled request', async () => {
    const controller = new AbortController()
    const fetcher = vi.fn((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal!.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
    }))
    vi.stubGlobal('fetch', fetcher)
    const assertion = expect(checkHealth(controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await assertion
    await expect(checkHealth(controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetcher).toHaveBeenCalledOnce()
  })

  it('cleans up its timer and abort listener after success', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    let requestSignal: AbortSignal | undefined
    vi.stubGlobal('fetch', vi.fn((_url, init: RequestInit) => {
      requestSignal = init.signal!
      return Promise.resolve(response({ status: 'ok' }))
    }))
    await checkHealth(controller.signal)
    controller.abort()
    expect(requestSignal?.aborted).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })
})
