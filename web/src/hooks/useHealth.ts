import { useCallback, useEffect, useRef, useState } from 'react'
import { checkHealth } from '../api/health'
import type { ConnectionState } from '../components/StatusPanel'

export function useHealth() {
  const [state, setState] = useState<ConnectionState>('idle')
  const [message, setMessage] = useState<string>()
  const active = useRef<AbortController | null>(null)

  useEffect(() => () => {
    active.current?.abort()
    active.current = null
  }, [])

  const check = useCallback(async () => {
    if (active.current) return
    const controller = new AbortController()
    active.current = controller
    setMessage(undefined)
    setState('loading')
    try {
      await checkHealth(controller.signal)
      if (active.current === controller) setState('online')
    } catch (error) {
      if (active.current === controller && !controller.signal.aborted) {
        setMessage(error instanceof Error ? error.message : '暂时无法连接服务，请重试。')
        setState('error')
      }
    } finally {
      if (active.current === controller) active.current = null
    }
  }, [])

  return { state, message, check }
}
