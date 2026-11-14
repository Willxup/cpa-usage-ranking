import { screen, within } from '@testing-library/react'
import { renderWithSession as render } from './renderWithSession'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../App'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const empty = { board: 'overall', period: 'today', start: '2026-12-31', end: '2027-01-01', generated_at: '2026-12-31T12:00:00Z', entries: [] }
const catalog = { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] }

afterEach(() => vi.unstubAllGlobals())

describe('application shell', () => {
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

  it('marks an earlier result while switching away and back', async () => {
    let first = true
    let finish!: (response: Response) => void
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/admin/session')) return Promise.resolve(json({ enabled: false, authenticated: false }))
      if (first) { first = false; return Promise.resolve(json(empty)) }
      if (url.includes('/overall/')) return new Promise<Response>((resolve) => { finish = resolve })
      return new Promise<Response>(() => {})
    }))
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    await user.click(screen.getByRole('button', { name: '用量榜' }))
    await user.click(screen.getByRole('button', { name: '综合总榜' }))
    expect(screen.getByText(/正在加载今日 · 综合总榜/)).toBeVisible()
    expect(screen.getByRole('heading', { name: /今日 · 综合总榜/ })).toHaveTextContent('上次结果')
    finish(json(empty))
    expect(await screen.findByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
  })

  it('keeps landmarks and manual liveness separate from readiness', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input) === '/healthz' ? { status: 'ok' } : String(input).includes('avatars') ? catalog : empty))))
    const user = userEvent.setup()
    render(<App />)
    const navigation = screen.getByRole('navigation', { name: '页面导航' })
    for (const link of navigation.querySelectorAll('a')) expect(document.getElementById(link.getAttribute('href')!.slice(1))).not.toBeNull()
    expect(screen.queryByRole('link', { name: '跳到主要内容' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '检查服务连接' }))
    const panel = screen.getByRole('region', { name: '保持连接。' })
    expect(await within(panel).findByText('服务在线')).toBeVisible()
    expect(within(panel).getByText('以实际加载为准')).toBeVisible()
  })
})
