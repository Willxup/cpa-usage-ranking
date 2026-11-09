import { render, screen, within } from '@testing-library/react'
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
    expect(await screen.findByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '今日 · 综合总榜' })).toBeVisible()
    expect(fetcher).toHaveBeenCalledWith('/v1/leaderboards/overall/today', expect.anything())
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByText('参与客户端')).not.toBeInTheDocument()
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
