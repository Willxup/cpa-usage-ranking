import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LeaderboardPanel } from '../LeaderboardPanel'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const catalog = { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }, { avatar_id: 2, path: 'images/002.webp' }, { avatar_id: 3, path: 'https://evil.test/image' }] }
const data = { board: 'usage', period: 'today', start: '2026-12-31', end: '2027-01-01', generated_at: '2026-12-31T12:00:00Z', entries: [{ rank: 1, client_id: '9007199254740993', display_name: '<script>alert(1)</script>', avatar_id: '2', metric: '9007199254740993' }] }
afterEach(() => vi.unstubAllGlobals())

describe('live leaderboard', () => {
  it('renders large integers exactly, text names and catalog avatar fallback', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : data))))
    const { container } = render(<LeaderboardPanel board="usage" period="today" />)
    expect(await screen.findByText('9,007,199,254,740,993')).toBeVisible()
    expect(screen.getByText('<script>alert(1)</script>')).toBeVisible()
    expect(container.querySelector('script')).toBeNull()
    const image = await waitFor(() => {
      const found = container.querySelector('.avatar img') as HTMLImageElement | null
      expect(found?.getAttribute('src')).toBe('/avatars/images/002.webp')
      return found!
    })
    fireEvent.error(image)
    expect(image.getAttribute('src')).toBe('/avatars/images/001.webp')
    fireEvent.error(image)
    expect(image.hidden).toBe(true)
    expect(screen.getByText(/缓存生成于 2026-12-31 12:00:00 UTC/)).toBeVisible()
  })

  it('uses the catalog default for an unknown avatar and never loads a catalog URL', async () => {
    const unknown = { ...data, entries: [{ ...data.entries[0], avatar_id: '3' }] }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(json(String(input).includes('avatars') ? catalog : unknown))))
    const { container } = render(<LeaderboardPanel board="usage" period="today" />)
    await screen.findByText('9,007,199,254,740,993')
    await waitFor(() => expect(container.querySelector('.avatar img')?.getAttribute('src')).toBe('/avatars/images/001.webp'))
  })

  it('cancels superseded requests and never labels old data as the new board', async () => {
    const pending = new Map<string, { resolve: (response: Response) => void; signal: AbortSignal }>()
    const overall = { ...data, board: 'overall', entries: [{ ...data.entries[0], display_name: '旧总榜', metric: '12.34' }] }
    const success = { ...overall, board: 'success', entries: [{ ...overall.entries[0], display_name: '新成功榜', metric: '20.65' }] }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('avatars')) return Promise.resolve(json(catalog))
      if (url.includes('/overall/')) return Promise.resolve(json(overall))
      return new Promise<Response>((resolve) => pending.set(url, { resolve, signal: init!.signal as AbortSignal }))
    }))
    const view = render(<LeaderboardPanel board="overall" period="today" />)
    expect(await screen.findByText('旧总榜')).toBeVisible()
    view.rerender(<LeaderboardPanel board="usage" period="today" />)
    await waitFor(() => expect(pending.has('/v1/leaderboards/usage/today')).toBe(true))
    expect(screen.getByRole('heading', { name: /今日 · 综合总榜/ })).toHaveTextContent('上次结果')
    expect(screen.queryByRole('heading', { name: /今日 · 用量榜/ })).not.toBeInTheDocument()
    view.rerender(<LeaderboardPanel board="success" period="today" />)
    await waitFor(() => expect(pending.has('/v1/leaderboards/success/today')).toBe(true))
    expect(pending.get('/v1/leaderboards/usage/today')!.signal.aborted).toBe(true)
    pending.get('/v1/leaderboards/success/today')!.resolve(json(success))
    expect(await screen.findByText('新成功榜')).toBeVisible()
    pending.get('/v1/leaderboards/usage/today')!.resolve(json(data))
    await waitFor(() => expect(screen.getByText('新成功榜')).toBeVisible())
    expect(screen.queryByText('<script>alert(1)</script>')).not.toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '成功率得分' })).toBeVisible()
  })

  it('shows failure, permits retry and never invents empty results', async () => {
    let calls = 0
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).includes('avatars')) return Promise.resolve(json(catalog))
      calls++
      return Promise.resolve(calls === 1 ? json({}, 503) : json(data))
    }))
    render(<LeaderboardPanel board="usage" period="today" />)
    expect(await screen.findByText(/榜单缓存尚未就绪/)).toBeVisible()
    expect(screen.queryByText('等待第一份真实数据')).not.toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: '重试榜单' }))
    expect(await screen.findByText('9,007,199,254,740,993')).toBeVisible()
  })
})
