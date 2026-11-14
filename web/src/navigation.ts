import { useSyncExternalStore } from 'react'

const routes = new Set(['/', '/admin/login', '/admin/leaderboards', '/admin/users', '/admin/accounts'])
const changed = 'ranking:navigation'
const subscribe = (listener: () => void) => {
  window.addEventListener('popstate', listener)
  window.addEventListener(changed, listener)
  return () => { window.removeEventListener('popstate', listener); window.removeEventListener(changed, listener) }
}
export function usePathname() { return useSyncExternalStore(subscribe, () => window.location.pathname) }

export function navigate(path: string, replace = false) {
  if (!routes.has(path)) throw new Error('Unknown application route')
  if (window.location.pathname !== path) {
    window.history[replace ? 'replaceState' : 'pushState']({}, '', path)
    window.dispatchEvent(new Event(changed))
    document.scrollingElement?.scrollTo?.({ top: 0, behavior: 'instant' })
  }
}

export function isAppRoute(path: string) { return routes.has(path) }
