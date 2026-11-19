import { act, screen, waitFor, within } from '@testing-library/react'
import { renderWithSession as render } from './renderWithSession'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import { LOCALE_STORAGE_KEY } from '../i18n'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const empty = { board: 'overall', period: 'today', start: '2026-12-31', end: '2027-01-01', generated_at: '2026-12-31T12:00:00Z', entries: [] }
const catalog = { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] }

afterEach(() => vi.unstubAllGlobals())

describe('application shell', () => {
  it.each([
    [{ enabled: false, authenticated: false }, null, null],
    [{ enabled: true, authenticated: false }, 'Sign in', '/admin/login'],
    [{ enabled: true, authenticated: true }, 'Console', '/admin/leaderboards'],
  ])('keeps the public navigation ordered for session %j', async (session, label, href) => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('/admin/session') ? session : String(input).includes('avatars') ? catalog : empty))))
    render(<App />)
    const navigation = screen.getByRole('navigation', { name: 'Page navigation' })
    if (label) expect(await within(navigation).findByRole('link', { name: label })).toHaveAttribute('href', href)
    else await screen.findByRole('heading', { name: 'Today · Overall' })
    expect(within(navigation).getAllByRole('link').map((link) => link.textContent)).toEqual(label ? ['Leaderboards', label] : ['Leaderboards'])
    expect(within(navigation).getByRole('link', { name: 'Leaderboards' })).toHaveAttribute('aria-current', 'page')
    expect(within(navigation).queryByRole('link', { name: /About|关于/ })).not.toBeInTheDocument()
    const languageGroup = screen.getByRole('group', { name: 'Language' })
    expect(navigation.contains(languageGroup)).toBe(false)
    expect(navigation.parentElement).toBe(languageGroup.parentElement)
  })

  it('loads the real empty view in English by default without fabricated counts or filters', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty)))
    vi.stubGlobal('fetch', fetcher)
    render(<App />)
    expect(document.documentElement.lang).toBe('en')
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('CPA Usage')
    expect(screen.queryByText('每一次调用，都将在这里被看见。')).not.toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Waiting for first usage report' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Today · Overall' })).toBeVisible()
    expect(fetcher).toHaveBeenCalledWith('/v1/leaderboards/overall/today', expect.anything())
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByText('参与客户端')).not.toBeInTheDocument()
  })

  it('toggles between English and Chinese in place and persists to localStorage', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty)))
    vi.stubGlobal('fetch', fetcher)
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Today · Overall' })).toBeVisible()
    const callsBefore = fetcher.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Switch language to Chinese' }))
    expect(document.documentElement.lang).toBe('zh-CN')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('CPA 用量榜')
    expect(screen.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
    expect(fetcher.mock.calls.length).toBe(callsBefore)
    await user.click(screen.getByRole('button', { name: '切换为英文' }))
    expect(document.documentElement.lang).toBe('en')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('CPA Usage')
    expect(screen.getByRole('heading', { name: 'Today · Overall' })).toBeVisible()
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
    expect(await screen.findByRole('heading', { name: 'Today · Overall' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Today' })).toHaveAttribute('aria-pressed', 'true')
    for (const name of ['Overall', 'Token Usage', 'Cache Reuse', 'Reliability', 'First-Token Latency']) expect(screen.getByRole('button', { name })).toBeEnabled()
    for (const name of ['Today', 'Yesterday', 'Weekly', 'Monthly']) expect(screen.getByRole('button', { name })).toBeEnabled()
    expect(screen.queryByRole('button', { name: /Annual|All-time|年度|累计/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Reliability' }))
    await user.click(screen.getByRole('button', { name: 'Yesterday' }))
    expect(await screen.findByRole('heading', { name: 'Yesterday · Reliability' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Yesterday' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps the displayed selection and rows until the latest board and period are ready', async () => {
    const pending = new Map<string, { resolve: (response: Response) => void; signal: AbortSignal }>()
    const row = { rank: 1, client_id: '1', display_name: 'Old Overall', avatar_id: '1', metric: '12.34' }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      if (url.endsWith('/overall/today')) return Promise.resolve(json({ ...empty, entries: [row] }))
      return new Promise<Response>((resolve) => pending.set(url, { resolve, signal: init!.signal as AbortSignal }))
    }))
    const user = userEvent.setup()
    render(<App />)
    const oldRow = await screen.findByRole('row', { name: /Old Overall/ })
    const oldResult = oldRow.closest('.leaderboard-result')
    await user.click(screen.getByRole('button', { name: 'Token Usage' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/today')).toBe(true))
    expect(screen.getByRole('heading', { name: 'Today · Overall' })).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Score' })).toBeVisible()
    expect(screen.getByRole('row', { name: /Old Overall/ })).toBe(oldRow)
    expect(oldRow.closest('.leaderboard-result')).toBe(oldResult)
    expect(screen.getByRole('button', { name: 'Overall' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Today' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText(/Loading|Previous result/)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Yesterday' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/yesterday')).toBe(true))
    await user.click(screen.getByRole('button', { name: 'Reliability' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/success/yesterday')).toBe(true))
    expect(pending.get('/v1/leaderboards/usage/today')!.signal.aborted).toBe(true)
    expect(pending.get('/v1/leaderboards/usage/yesterday')!.signal.aborted).toBe(true)
    await act(async () => pending.get('/v1/leaderboards/success/yesterday')!.resolve(json({ ...empty, board: 'success', period: 'yesterday', entries: [{ ...row, display_name: 'New Reliability', metric: '78.90' }] })))
    expect(await screen.findByRole('heading', { name: 'Yesterday · Reliability' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Reliability' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Yesterday' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('columnheader', { name: 'Reliability Score' })).toBeVisible()
    expect(screen.getByRole('row', { name: /New Reliability/ })).toHaveTextContent('78.90')
    expect(screen.getByRole('row', { name: /New Reliability/ }).closest('.leaderboard-result')).not.toBe(oldResult)
    await act(async () => {
      pending.get('/v1/leaderboards/usage/today')!.resolve(json({ ...empty, board: 'usage', entries: [{ ...row, metric: '1234' }] }))
      pending.get('/v1/leaderboards/usage/yesterday')!.resolve(json({ ...empty, board: 'usage', period: 'yesterday', entries: [{ ...row, metric: '1234' }] }))
    })
    expect(screen.getByRole('heading', { name: 'Yesterday · Reliability' })).toBeVisible()
    expect(screen.getByRole('row', { name: /New Reliability/ })).toBeVisible()
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
    expect(screen.queryByText(/Loading|LOADING/)).not.toBeInTheDocument()
    await waitFor(() => expect(pending.has('/v1/leaderboards/overall/today')).toBe(true))
    await act(async () => pending.get('/v1/leaderboards/overall/today')!(json({}, 503)))
    expect(await screen.findByRole('button', { name: 'Retry leaderboard' })).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Token Usage' }))
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/today')).toBe(true))
    expect(screen.queryByText(/Loading|LOADING/)).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await act(async () => pending.get('/v1/leaderboards/usage/today')!(json({ ...empty, board: 'usage' })))
    expect(await screen.findByRole('heading', { name: 'Today · Token Usage' })).toBeVisible()
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
    await screen.findByRole('heading', { name: 'Today · Overall' })
    await user.click(screen.getByRole('button', { name: 'Cache Reuse' }))
    expect(await screen.findByText(/Leaderboard cache is not ready yet/)).toBeVisible()
    expect(screen.getByRole('heading', { name: /Today · Overall/ })).toHaveTextContent('Previous result')
    expect(screen.getByRole('button', { name: 'Overall' })).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: 'Yesterday' }))
    expect(await screen.findByRole('heading', { name: 'Yesterday · Overall' })).toBeVisible()
    expect(fetcher).toHaveBeenCalledWith('/v1/leaderboards/overall/yesterday', expect.anything())
  })

  it('keeps only the project logo and name in the public footer', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty))))
    render(<App />)
    const footer = screen.getByRole('contentinfo')
    expect(footer).toHaveTextContent(/^CPA Usage Ranking$/)
    expect(footer.querySelector('img')).toHaveAttribute('src', '/assets/brand/logo.svg')
    expect(footer.querySelector('p')).not.toBeInTheDocument()
    await screen.findByRole('heading', { name: 'Today · Overall' })
  })

  it('shows only the leaderboard without an introduction or health check', async () => {
    const fetcher = vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : empty)))
    vi.stubGlobal('fetch', fetcher)
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Today · Overall' })).toBeVisible()
    expect(screen.queryByText('ABOUT THE RANKING')).not.toBeInTheDocument()
    expect(screen.queryByText('LIVE CONNECTION')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Check|检查服务连接/ })).not.toBeInTheDocument()
    expect(fetcher.mock.calls.some(([path]) => String(path) === '/healthz')).toBe(false)
  })

  it('retries a missing avatar catalog when the user switches or retries the leaderboard', async () => {
    let failCatalog = true
    const row = { rank: 1, client_id: '1', display_name: 'Participant A', avatar_id: '1', metric: '12.34' }
    const fetcher = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      if (url.includes('avatars')) return failCatalog ? Promise.resolve(json({}, 503)) : Promise.resolve(json(catalog))
      const [, board = 'overall', period = 'today'] = url.match(/leaderboards\/([^/]+)\/([^/]+)/) ?? []
      return Promise.resolve(json({ ...empty, board, period, entries: [row] }))
    })
    vi.stubGlobal('fetch', fetcher)
    const user = userEvent.setup()
    render(<App />)
    const cell = await screen.findByRole('cell', { name: 'Participant A' })
    expect(cell.querySelector('img')).toBeNull()
    failCatalog = false
    await user.click(screen.getByRole('button', { name: 'Token Usage' }))
    await waitFor(() => expect(screen.getByRole('cell', { name: 'Participant A' }).querySelector('img')).toHaveAttribute('src', '/avatars/images/001.webp'))
  })
})
