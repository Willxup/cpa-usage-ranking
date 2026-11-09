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
