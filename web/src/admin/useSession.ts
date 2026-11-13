import { useCallback, useEffect, useState } from 'react'
import { adminRequest, type AdminSession } from './api'

// Only safe enable/session state is fetched on the public page. No credentials
// or administrator data are put into localStorage or a public config object.
export function useSession() {
  const [session, setSession] = useState<AdminSession | null>(null)
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    try { setSession(await adminRequest<AdminSession>('session')); setError('') }
    catch (error) { setError((error as Error).message) }
  }, [])
  useEffect(() => {
    let active = true
    const check = () => { if (active) void refresh() }
    check()
    window.addEventListener('focus', check)
    const visible = () => { if (document.visibilityState === 'visible') check() }
    document.addEventListener('visibilitychange', visible)
    const interval = window.setInterval(check, 60_000)
    return () => { active = false; window.clearInterval(interval); window.removeEventListener('focus', check); document.removeEventListener('visibilitychange', visible) }
  }, [refresh])
  useEffect(() => {
    if (!session?.expires_at) return
    const delay = Math.max(0, Date.parse(session.expires_at) - Date.now())
    const timeout = window.setTimeout(() => { setSession({ enabled: true, authenticated: false }); void refresh() }, delay)
    return () => window.clearTimeout(timeout)
  }, [session?.expires_at, refresh])
  return { session, error, refresh }
}
