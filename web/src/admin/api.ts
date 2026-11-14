export type AdminSession = { enabled: boolean; authenticated: boolean; user?: { id: string; username: string; owner: boolean }; csrf_token?: string; expires_at?: string }

export class AdminError extends Error {
  status: number
  constructor(status: number, message: string) { super(message); this.status = status }
}

export async function adminRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 10_000)
  try {
    const response = await fetch(`/v1/admin/${path}`, { ...init, credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
    if (!response.ok) {
      const messages: Record<number, string> = { 400: '输入无效，请检查后重试。', 401: path === 'session' && init.method === 'POST' ? '用户名或密码错误。' : '会话已过期，请重新登录。', 403: '无操作权限或安全校验失败，请刷新后重试。', 404: '目标不存在，请刷新后重试。', 409: path.startsWith('users/') ? '该用户仍被封禁，请先到用户管理解封。' : '该用户名已存在或不可使用。', 429: '登录尝试过多，请稍后再试。' }
      throw new AdminError(response.status, messages[response.status] ?? `服务暂时不可用（${response.status}），请重试。`)
    }
    return response.status === 204 ? undefined as T : await response.json() as T
  } catch (error) {
    if (error instanceof AdminError) throw error
    throw new Error('连接失败或请求超时，请检查连接后重试。')
  } finally { window.clearTimeout(timeout) }
}

export function jsonRequest(method: string, value: unknown, csrf?: string): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json', ...(csrf ? { 'X-CSRF-Token': csrf } : {}) }, body: JSON.stringify(value) }
}
