import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { useHealth } from '../useHealth'

const ok = () => new Response('{"status":"ok"}', { headers: { 'Content-Type': 'application/json' } })

describe('useHealth', () => {
  it('does not poll or make requests before a user action', () => {
    const fetcher = vi.fn()
    vi.stubGlobal('fetch', fetcher)
    const { result } = renderHook(useHealth, { wrapper: StrictMode })
    expect(result.current.state).toBe('idle')
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('prevents duplicate in-flight checks and publishes success', async () => {
    let resolve!: (value: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>((done) => { resolve = done }))
    vi.stubGlobal('fetch', fetcher)
    const { result } = renderHook(useHealth)
    act(() => { void result.current.check(); void result.current.check() })
    expect(result.current.state).toBe('loading')
    expect(fetcher).toHaveBeenCalledOnce()
    await act(async () => resolve(ok()))
    await waitFor(() => expect(result.current.state).toBe('online'))
  })

  it('clears an error and supports an explicit retry', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(ok()))
    const { result } = renderHook(useHealth)
    await act(() => result.current.check())
    expect(result.current.state).toBe('error')
    expect(result.current.message).toContain('无法连接服务')
    await act(() => result.current.check())
    expect(result.current.state).toBe('online')
    expect(result.current.message).toBeUndefined()
  })

  it('cancels the active request when unmounted', async () => {
    let requestSignal: AbortSignal | undefined
    vi.stubGlobal('fetch', vi.fn((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      requestSignal = init.signal!
      requestSignal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
    })))
    const { result, unmount } = renderHook(useHealth, { wrapper: StrictMode })
    let pending!: Promise<void>
    act(() => { pending = result.current.check() })
    unmount()
    await pending
    expect(requestSignal?.aborted).toBe(true)
  })
})
