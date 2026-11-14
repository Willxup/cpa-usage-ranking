import { createContext } from 'react'
import type { AdminSession } from './api'

export type SessionState = { session: AdminSession | null; error: string; refresh: () => Promise<void>; accept: (session: AdminSession) => void }
export const SessionContext = createContext<SessionState | null>(null)
