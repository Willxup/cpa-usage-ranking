import { useEffect, useState, type RefObject } from 'react'

// Default to a static control; animate only while visible and explicitly permitted.
export function useDecorativeMotion(ref: RefObject<HTMLElement | null>): boolean {
  const [allowed, setAllowed] = useState(false)
  useEffect(() => {
    const element = ref.current
    if (!element || typeof window.matchMedia !== 'function' || typeof IntersectionObserver !== 'function') return
    const preference = window.matchMedia('(prefers-reduced-motion: reduce)')
    let intersecting = false
    let disposed = false
    const update = () => {
      if (!disposed) setAllowed(intersecting && !preference.matches && document.visibilityState === 'visible')
    }
    const observer = new IntersectionObserver((entries) => {
      intersecting = entries.some((entry) => entry.target === element && entry.isIntersecting)
      update()
    })
    preference.addEventListener('change', update)
    document.addEventListener('visibilitychange', update)
    observer.observe(element)
    return () => {
      disposed = true
      observer.disconnect()
      preference.removeEventListener('change', update)
      document.removeEventListener('visibilitychange', update)
    }
  }, [ref])
  return allowed
}
