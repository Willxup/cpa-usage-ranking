import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { formatLocalizedError, useI18n } from '../i18n'
import { adminRequest, type AdminSession } from './api'
import { SessionContext } from './sessionContext'

// One live in-memory session for this document, shared by public and console
// navigation. It is never persisted or trusted instead of server authorization.
export function SessionProvider({ children }: { children: ReactNode }) {
  const { locale } = useI18n()
  const [session, setSession] = useState<AdminSession | null>(null)
  const [rawError, setRawError] = useState<unknown>(null)
  const error = rawError == null ? '' : formatLocalizedError(rawError, (m) => m.admin.errors.network, locale)
  const generation = useRef(0)
  const active = useRef(false)
  const pending = useRef<Promise<void> | null>(null)
  const accept = useCallback((value: AdminSession) => {
    generation.current++
    const expired = value.expires_at && Date.parse(value.expires_at) <= Date.now()
    setSession(expired ? { enabled: value.enabled, authenticated: false } : value)
    setRawError(null)
  }, [])
  const refresh = useCallback(() => {
    if (pending.current) return pending.current
    const ticket = ++generation.current
    const request = adminRequest<AdminSession>('session').then((value) => {
      if (active.current && ticket === generation.current) accept(value)
    }).catch((err: unknown) => { if (active.current && ticket === generation.current) setRawError(err ?? true) }).finally(() => { if (pending.current === request) pending.current = null })
    pending.current = request
    return request
  }, [accept])
  useEffect(() => {
    active.current = true
    const check = () => { void refresh() }
    const visible = () => { if (document.visibilityState === 'visible') check() }
    const expired = () => accept({ enabled: true, authenticated: false })
    check()
    window.addEventListener('focus', check)
    document.addEventListener('visibilitychange', visible)
    window.addEventListener('ranking:session-expired', expired)
    const interval = window.setInterval(check, 60_000)
    return () => { active.current = false; pending.current = null; window.clearInterval(interval); window.removeEventListener('focus', check); document.removeEventListener('visibilitychange', visible); window.removeEventListener('ranking:session-expired', expired) }
  }, [refresh, accept])
  useEffect(() => {
    if (!session?.expires_at) return
    const timeout = window.setTimeout(() => accept({ enabled: session.enabled, authenticated: false }), Math.max(0, Date.parse(session.expires_at) - Date.now()))
    return () => window.clearTimeout(timeout)
  }, [session?.expires_at, session?.enabled, accept])
  return <SessionContext value={{ session, error, refresh, accept }}>{children}</SessionContext>
}
