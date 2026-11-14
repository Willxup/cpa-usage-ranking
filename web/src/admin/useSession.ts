import { useContext } from 'react'
import { SessionContext } from './sessionContext'

export function useSession() {
  const state = useContext(SessionContext)
  if (!state) throw new Error('SessionProvider is required')
  return state
}
