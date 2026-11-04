import { act, renderHook } from '@testing-library/react'
import { StrictMode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { useDecorativeMotion } from '../useDecorativeMotion'

function environment() {
  const element = document.createElement('button')
  const ref = { current: element }
  const media = new EventTarget()
  let reduced = false
  let visible = true
  const observers: { emit: (visible: boolean) => void; disconnect: ReturnType<typeof vi.fn> }[] = []
  const removePreference = vi.spyOn(media, 'removeEventListener')
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visible ? 'visible' : 'hidden')
  vi.stubGlobal('matchMedia', vi.fn(() => ({
    get matches() { return reduced },
    addEventListener: media.addEventListener.bind(media),
    removeEventListener: media.removeEventListener.bind(media),
  })))
  vi.stubGlobal('IntersectionObserver', class {
    disconnect = vi.fn()
    observe = vi.fn()
    constructor(callback: IntersectionObserverCallback) {
      observers.push({
        disconnect: this.disconnect,
        emit: (isIntersecting) => callback([{
          target: element, isIntersecting, time: 0, rootBounds: null,
          boundingClientRect: element.getBoundingClientRect(),
          intersectionRect: element.getBoundingClientRect(),
          intersectionRatio: isIntersecting ? 1 : 0,
        }], this as unknown as IntersectionObserver),
      })
    }
  })
  return {
    ref, observers, removePreference,
    reduced(value: boolean) { reduced = value; media.dispatchEvent(new Event('change')) },
    visible(value: boolean) { visible = value; document.dispatchEvent(new Event('visibilitychange')) },
  }
}

describe('useDecorativeMotion', () => {
  it('is static until visible, and stops offscreen or in a hidden document', () => {
    const env = environment()
    const { result } = renderHook(() => useDecorativeMotion(env.ref))
    expect(result.current).toBe(false)
    act(() => env.observers[0].emit(true))
    expect(result.current).toBe(true)
    act(() => env.visible(false))
    expect(result.current).toBe(false)
    act(() => env.visible(true))
    expect(result.current).toBe(true)
    act(() => env.observers[0].emit(false))
    expect(result.current).toBe(false)
  })

  it('responds to reduced-motion changes without requiring a reload', () => {
    const env = environment()
    env.reduced(true)
    const { result } = renderHook(() => useDecorativeMotion(env.ref))
    act(() => env.observers[0].emit(true))
    expect(result.current).toBe(false)
    act(() => env.reduced(false))
    expect(result.current).toBe(true)
    act(() => env.reduced(true))
    expect(result.current).toBe(false)
  })

  it('disconnects observers and listeners across StrictMode remount and final unmount', () => {
    const env = environment()
    const removeVisibility = vi.spyOn(document, 'removeEventListener')
    const { unmount } = renderHook(() => useDecorativeMotion(env.ref), { wrapper: StrictMode })
    expect(env.observers).toHaveLength(2)
    expect(env.observers[0].disconnect).toHaveBeenCalledOnce()
    unmount()
    expect(env.observers[1].disconnect).toHaveBeenCalledOnce()
    expect(env.removePreference).toHaveBeenCalledTimes(2)
    expect(removeVisibility.mock.calls.filter(([name]) => name === 'visibilitychange')).toHaveLength(2)
  })

  it('keeps the control functional and static when observer APIs are unavailable', () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    const { result } = renderHook(() => useDecorativeMotion({ current: document.createElement('button') }))
    expect(result.current).toBe(false)
  })
})
