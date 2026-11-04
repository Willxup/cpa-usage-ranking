class HealthCheckError extends Error {}

// Manual, same-origin liveness only: no polling, credentials, or business readiness claims.
export async function checkHealth(signal: AbortSignal): Promise<void> {
  signal.throwIfAborted()
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal.addEventListener('abort', cancel, { once: true })
  let timedOut = false
  const timeout = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, 8_000)
  try {
    const response = await fetch('/healthz', {
      signal: controller.signal,
      cache: 'no-store',
      credentials: 'omit',
      redirect: 'error',
      headers: { Accept: 'application/json' },
    })
    if (!response.ok) throw new HealthCheckError(`服务返回 ${response.status}，请稍后重试。`)
    if (response.headers.get('content-type')?.split(';')[0].trim() !== 'application/json') {
      throw new HealthCheckError('健康检查响应无效，请检查服务配置。')
    }
    let data: unknown
    try { data = await response.json() } catch { throw new HealthCheckError('健康检查响应无效，请检查服务配置。') }
    if (typeof data !== 'object' || data === null || !('status' in data) || data.status !== 'ok') {
      throw new HealthCheckError('健康检查响应无效，请检查服务配置。')
    }
  } catch (error) {
    if (signal.aborted) throw signal.reason
    if (timedOut) throw new HealthCheckError('连接超时，请稍后重试。')
    if (error instanceof HealthCheckError) throw error
    throw new HealthCheckError('无法连接服务，请检查网络或稍后重试。')
  } finally {
    clearTimeout(timeout)
    signal.removeEventListener('abort', cancel)
  }
}
