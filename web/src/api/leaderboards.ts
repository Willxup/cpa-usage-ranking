import { LocalizedError } from '../i18n'

export type Board = 'overall' | 'usage' | 'cache' | 'success' | 'ttft'
export type Period = 'today' | 'yesterday' | 'week' | 'month'
export type OverallDetails = { total_tokens: string; request_count: string; success_rate: string; cache_read_rate: string | null; ttft_mean_ms: string | null; latency_mean_ms: string | null }
export type Entry = { rank: number; client_id: string; display_name: string; avatar_id: string; metric: string; details?: OverallDetails }
export type Leaderboard = { board: Board; period: Period; start: string; end: string; generated_at: string; entries: Entry[] }
export type AvatarCatalog = { defaultPath: string; paths: Map<string, string> }

const digits = /^[1-9]\d*$/
const object = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value)
const count = (value: unknown) => typeof value === 'string' && /^(0|[1-9]\d{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n
const percent = (value: unknown) => typeof value === 'string' && /^\d{1,3}\.\d{2}$/.test(value) && Number(value) <= 100
const mean = (value: unknown) => value === null || (typeof value === 'string' && /^\d+\.\d{2}$/.test(value) && Number.isFinite(Number(value)))
function validDetails(value: unknown): value is OverallDetails {
  return object(value) && count(value.total_tokens) && count(value.request_count) && percent(value.success_rate) && (value.cache_read_rate === null || percent(value.cache_read_rate)) && mean(value.ttft_mean_ms) && mean(value.latency_mean_ms)
}

export async function fetchLeaderboard(board: Board, period: Period, signal: AbortSignal): Promise<Leaderboard> {
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal.addEventListener('abort', cancel, { once: true })
  const timer = setTimeout(() => controller.abort(), 8_000)
  try {
    signal.throwIfAborted()
    const response = await fetch(`/v1/leaderboards/${board}/${period}`, { signal: controller.signal, cache: 'no-store', credentials: 'omit', headers: { Accept: 'application/json' } })
    if (!response.ok) {
      throw response.status === 503
        ? new LocalizedError((m) => m.leaderboardErrors.notReady)
        : new LocalizedError((m) => m.leaderboardErrors.statusFailed(response.status))
    }
    if (response.headers.get('content-type')?.split(';')[0].trim() !== 'application/json') throw new LocalizedError((m) => m.leaderboardErrors.invalidResponse)
    let data: unknown
    try { data = await response.json() } catch { throw new LocalizedError((m) => m.leaderboardErrors.invalidResponse) }
    if (!object(data) || data.board !== board || data.period !== period || typeof data.start !== 'string' || typeof data.end !== 'string' || typeof data.generated_at !== 'string' || !Array.isArray(data.entries) || data.entries.length > 100) throw new LocalizedError((m) => m.leaderboardErrors.invalidResponse)
    const entries: Entry[] = data.entries.map((entry: unknown) => {
      if (!object(entry) || !Number.isInteger(entry.rank) || (entry.rank as number) < 1 || typeof entry.client_id !== 'string' || !digits.test(entry.client_id) || typeof entry.avatar_id !== 'string' || !digits.test(entry.avatar_id) || typeof entry.display_name !== 'string' || typeof entry.metric !== 'string' || !(board === 'usage' ? /^\d+$/ : /^\d+\.\d{2}$/).test(entry.metric)) throw new LocalizedError((m) => m.leaderboardErrors.invalidResponse)
      if (board === 'overall' && entry.details !== undefined && !validDetails(entry.details)) throw new LocalizedError((m) => m.leaderboardErrors.invalidResponse)
      return entry as Entry
    })
    return { board, period, start: data.start, end: data.end, generated_at: data.generated_at, entries }
  } catch (error) {
    if (signal.aborted) throw signal.reason
    if (controller.signal.aborted) throw new LocalizedError((m) => m.leaderboardErrors.timeout)
    if (error instanceof TypeError) throw new LocalizedError((m) => m.leaderboardErrors.network)
    if (error instanceof Error) throw error
    throw new LocalizedError((m) => m.leaderboardErrors.generic)
  } finally {
    clearTimeout(timer)
    signal.removeEventListener('abort', cancel)
  }
}

// 头像只能来自同源目录列出的相对资源；缺项使用目录默认项。
export async function fetchAvatarCatalog(signal: AbortSignal): Promise<AvatarCatalog> {
  const response = await fetch('/avatars/catalog.json', { signal, cache: 'no-cache', credentials: 'omit' })
  if (!response.ok) throw new Error('avatar catalog unavailable')
  const value: unknown = await response.json()
  if (!object(value) || !Number.isSafeInteger(value.default_avatar_id) || !Array.isArray(value.avatars)) throw new Error('invalid avatar catalog')
  const paths = new Map<string, string>()
  for (const item of value.avatars) {
    if (!object(item) || !Number.isSafeInteger(item.avatar_id) || typeof item.path !== 'string' || !/^images\/[a-zA-Z0-9_./-]+$/.test(item.path) || item.path.split('/').some((part) => part === '.' || part === '..' || part === '')) continue
    paths.set(String(item.avatar_id), `/avatars/${item.path}`)
  }
  const defaultPath = paths.get(String(value.default_avatar_id))
  if (!defaultPath) throw new Error('invalid default avatar')
  return { defaultPath, paths }
}
