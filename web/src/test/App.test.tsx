import { act, screen, waitFor, within } from '@testing-library/react'
import { renderWithSession as render } from './renderWithSession'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../App'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const empty = { board: 'overall', period: 'today', start: '2026-12-31', end: '2027-01-01', generated_at: '2026-12-31T12:00:00Z', entries: [] }
const catalog = { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] }

afterEach(() => vi.unstubAllGlobals())

describe('application shell', () => {
  it.each([
    [{ enabled: false, authenticated: false }, null, null],
    [{ enabled: true, authenticated: false }, '登录', '/admin/login'],
    [{ enabled: true, authenticated: true }, '控制台', '/admin/leaderboards'],
  ])('keeps the public navigation ordered for session %j', async (session, label, href) => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('/admin/session') ? session : String(input).includes('avatars') ? catalog : empty))))
    render(<App />)
    const navigation = screen.getByRole('navigation', { name: '页面导航' })
    if (label) expect(await within(navigation).findByRole('link', { name: label })).toHaveAttribute('href', href)
    else await screen.findByRole('heading', { name: '今日 · 综合总榜' })
    expect(within(navigation).getAllByRole('link').map((link) => link.textContent)).toEqual(label ? ['排行榜', label] : ['排行榜'])
    expect(within(navigation).getByRole('link', { name: '排行榜' })).toHaveAttribute('aria-current', 'page')
    expect(within(navigation).queryByRole('link', { name: '关于' })).not.toBeInTheDocument()
  })

  it('loads the real empty view without fabricated counts or filters', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty)))
    vi.stubGlobal('fetch', fetcher)
    render(<App />)
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('AI 用量排行榜')
    expect(screen.queryByText('每一次调用，都将在这里被看见。')).not.toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(fetcher).toHaveBeenCalledWith('/v1/leaderboards/overall/today', expect.anything())
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByText('参与客户端')).not.toBeInTheDocument()
  })

  it('offers only five boards and four periods, defaulting to today', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      const [, board, period] = url.match(/leaderboards\/([^/]+)\/([^/]+)/) ?? []
      return Promise.resolve(json({ ...empty, board, period }))
    }))
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(screen.getByRole('button', { name: '今日' })).toHaveAttribute('aria-pressed', 'true')
    for (const name of ['综合总榜', '用量榜', '缓存复用榜', '成功率榜', '首字响应榜']) expect(screen.getByRole('button', { name })).toBeEnabled()
    for (const name of ['今日', '昨日', '本周', '本月']) expect(screen.getByRole('button', { name })).toBeEnabled()
    expect(screen.queryByRole('button', { name: /年度|累计/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '成功率榜' }))
    await user.click(screen.getByRole('button', { name: '昨日' }))
    expect(await screen.findByRole('heading', { name: '昨日 · 成功率榜' })).toBeVisible()
    expect(screen.getByRole('button', { name: '昨日' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps the displayed selection and rows until the latest board and period are ready', async () => {
    const pending = new Map<string, { resolve: (response: Response) => void; signal: AbortSignal }>()
    const row = { rank: 1, client_id: '1', display_name: '原总榜', avatar_id: '1', metric: '12.34' }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      if (url.endsWith('/overall/today')) return Promise.resolve(json({ ...empty, entries: [row] }))
      return new Promise<Response>((resolve) => pending.set(url, { resolve, signal: init!.signal as AbortSignal }))
    }))
    const user = userEvent.setup()
    render(<App />)
    const oldRow = await screen.findByRole('row', { name: /原总榜/ })
    const oldResult = oldRow.closest('.leaderboard-result')
    await user.click(screen.getByRole('button', { name: '用量榜' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/today')).toBe(true))
    expect(screen.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(screen.getByRole('columnheader', { name: '总分' })).toBeVisible()
    expect(screen.getByRole('row', { name: /原总榜/ })).toBe(oldRow)
    expect(oldRow.closest('.leaderboard-result')).toBe(oldResult)
    expect(screen.getByRole('button', { name: '综合总榜' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '今日' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText(/正在加载|上次结果/)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '昨日' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/yesterday')).toBe(true))
    await user.click(screen.getByRole('button', { name: '成功率榜' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/success/yesterday')).toBe(true))
    expect(pending.get('/v1/leaderboards/usage/today')!.signal.aborted).toBe(true)
    expect(pending.get('/v1/leaderboards/usage/yesterday')!.signal.aborted).toBe(true)
    await act(async () => pending.get('/v1/leaderboards/success/yesterday')!.resolve(json({ ...empty, board: 'success', period: 'yesterday', entries: [{ ...row, display_name: '新成功榜', metric: '78.90' }] })))
    expect(await screen.findByRole('heading', { name: '昨日 · 成功率榜' })).toBeVisible()
    expect(screen.getByRole('button', { name: '成功率榜' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '昨日' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('columnheader', { name: '成功率得分' })).toBeVisible()
    expect(screen.getByRole('row', { name: /新成功榜/ })).toHaveTextContent('78.90')
    expect(screen.getByRole('row', { name: /新成功榜/ }).closest('.leaderboard-result')).not.toBe(oldResult)
    await act(async () => {
      pending.get('/v1/leaderboards/usage/today')!.resolve(json({ ...empty, board: 'usage', entries: [{ ...row, metric: '1234' }] }))
      pending.get('/v1/leaderboards/usage/yesterday')!.resolve(json({ ...empty, board: 'usage', period: 'yesterday', entries: [{ ...row, metric: '1234' }] }))
    })
    expect(screen.getByRole('heading', { name: '昨日 · 成功率榜' })).toBeVisible()
    expect(screen.getByRole('row', { name: /新成功榜/ })).toBeVisible()
  })

  it('stays quiet before the first result and when switching after an initial failure', async () => {
    const pending = new Map<string, (response: Response) => void>()
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      return new Promise<Response>((resolve) => pending.set(url, resolve))
    }))
    const user = userEvent.setup()
    render(<App />)
    expect(screen.queryByText(/正在加载|LOADING/)).not.toBeInTheDocument()
    await waitFor(() => expect(pending.has('/v1/leaderboards/overall/today')).toBe(true))
    await act(async () => pending.get('/v1/leaderboards/overall/today')!(json({}, 503)))
    expect(await screen.findByRole('button', { name: '重试榜单' })).toBeVisible()
    await user.click(screen.getByRole('button', { name: '用量榜' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/today')).toBe(true))
    expect(screen.queryByText(/正在加载|LOADING/)).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await act(async () => pending.get('/v1/leaderboards/usage/today')!(json({ ...empty, board: 'usage' })))
    expect(await screen.findByRole('heading', { name: '今日 · 用量榜' })).toBeVisible()
  })

  it('reports a failed switch and bases the next choice on the still-visible result', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      if (url.endsWith('/cache/today')) return Promise.resolve(json({}, 503))
      const [, board, period] = url.match(/leaderboards\/([^/]+)\/([^/]+)/) ?? []
      return Promise.resolve(json({ ...empty, board, period }))
    })
    vi.stubGlobal('fetch', fetcher)
    const user = userEvent.setup()
    render(<App />)
    await screen.findByRole('heading', { name: '今日 · 综合总榜' })
    await user.click(screen.getByRole('button', { name: '缓存复用榜' }))
    expect(await screen.findByText(/榜单缓存尚未就绪/)).toBeVisible()
    expect(screen.getByRole('heading', { name: /今日 · 综合总榜/ })).toHaveTextContent('上次结果')
    expect(screen.getByRole('button', { name: '综合总榜' })).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: '昨日' }))
    expect(await screen.findByRole('heading', { name: '昨日 · 综合总榜' })).toBeVisible()
    expect(fetcher).toHaveBeenCalledWith('/v1/leaderboards/overall/yesterday', expect.anything())
  })

  it('keeps only the project logo and name in the public footer', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty))))
    render(<App />)
    const footer = screen.getByRole('contentinfo')
    expect(footer).toHaveTextContent(/^CPA Usage Ranking$/)
    expect(footer.querySelector('img')).toHaveAttribute('src', '/assets/brand/project-icon.svg')
    expect(footer.querySelector('p')).not.toBeInTheDocument()
    await screen.findByRole('heading', { name: '今日 · 综合总榜' })
  })

  it('shows only the leaderboard without an introduction or health check', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty)))
    vi.stubGlobal('fetch', fetcher)
    render(<App />)
    expect(await screen.findByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(screen.queryByText('ABOUT THE RANKING')).not.toBeInTheDocument()
    expect(screen.queryByText('LIVE CONNECTION')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '检查服务连接' })).not.toBeInTheDocument()
    expect(fetcher.mock.calls.some(([path]) => String(path) === '/healthz')).toBe(false)
  })
})
