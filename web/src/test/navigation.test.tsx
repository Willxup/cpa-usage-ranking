import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import RootApp from '../RootApp'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const session = { enabled: true, authenticated: true, user: { id: 'owner', username: 'owner', owner: true }, csrf_token: 'csrf', expires_at: new Date(Date.now() + 3600000).toISOString() }
const empty = { board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T00:00:00Z', entries: [] }
const catalog = { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] }
afterEach(() => { vi.unstubAllGlobals(); window.history.replaceState({}, '', '/') })

it('keeps the document and console shell while sharing one session across tabs and public navigation', async () => {
  let finish!: (response: Response) => void
  const fetcher = vi.fn((path: RequestInfo | URL) => {
    if (String(path).includes('/admin/session')) return Promise.resolve(json(session))
    if (String(path).includes('/admin/users')) return new Promise<Response>((resolve) => { finish = resolve })
    if (String(path).includes('/admin/accounts')) return Promise.resolve(json({ owner: { username: 'owner' }, accounts: [] }))
    return Promise.resolve(json(String(path).includes('avatars') ? catalog : empty))
  })
  vi.stubGlobal('fetch', fetcher)
  const user = userEvent.setup()
  render(<RootApp />)
  await user.click(await screen.findByRole('link', { name: '控制台' }))
  const navigation = await screen.findByRole('navigation', { name: '控制台导航' })
  await user.click(screen.getByRole('link', { name: '用户管理' }))
  expect(await screen.findByRole('heading', { name: '用户管理' })).toBeVisible()
  expect(screen.getByRole('navigation', { name: '控制台导航' })).toBe(navigation)
  expect(screen.getByRole('main').textContent).not.toMatch(/正在加载|正在检查会话/)
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  expect(document.querySelector('.admin-progress')).toBeNull()
  finish(json({ users: [], next_cursor: '' }))
  await user.click(screen.getByRole('link', { name: '管理员管理' }))
  expect(await screen.findByRole('heading', { name: '管理员管理' })).toBeVisible()
  window.history.back()
  await waitFor(() => expect(window.location.pathname).toBe('/admin/users'))
  expect(await screen.findByRole('heading', { name: '用户管理' })).toBeVisible()
  window.history.forward()
  expect(await screen.findByRole('heading', { name: '管理员管理' })).toBeVisible()
  const reads = fetcher.mock.calls.filter(([path]) => String(path) === '/v1/leaderboards/overall/today').length
  await user.click(screen.getByRole('link', { name: '公开榜单' }))
  expect(screen.getByRole('heading', { name: 'AI 用量排行榜' })).toBeVisible()
  await waitFor(() => expect(fetcher.mock.calls.filter(([path]) => String(path) === '/v1/leaderboards/overall/today').length).toBeGreaterThan(reads))
  expect(fetcher.mock.calls.filter(([path]) => String(path) === '/v1/admin/session')).toHaveLength(1)
})

it('invalidates the shared identity on a protected 401 instead of trusting the previous screen', async () => {
  let expired = false
  vi.stubGlobal('fetch', vi.fn((path: RequestInfo | URL) => {
    if (String(path).includes('/admin/session')) return Promise.resolve(json(expired ? { enabled: true, authenticated: false } : session))
    if (String(path).includes('/admin/users')) { expired = true; return Promise.resolve(json({}, 401)) }
    return Promise.resolve(json(String(path).includes('avatars') ? catalog : empty))
  }))
  window.history.replaceState({}, '', '/admin/leaderboards')
  const user = userEvent.setup()
  render(<RootApp />)
  await user.click(await screen.findByRole('link', { name: '用户管理' }))
  expect(await screen.findByRole('heading', { name: '管理员登录' })).toBeVisible()
  expect(window.location.pathname).toBe('/admin/login')
  expect(screen.queryByRole('navigation', { name: '控制台导航' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('link', { name: '公开榜单' }))
  expect(await screen.findByRole('link', { name: '登录' })).toBeVisible()
})
