import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { AdminEntry } from './AdminEntry'
import AdminApp from './AdminApp'

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
afterEach(() => { vi.unstubAllGlobals(); window.history.replaceState({}, '', '/') })

it('hides the disabled entry and shows login or console from session state', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ enabled: false, authenticated: false })))
  const first = render(<AdminEntry />)
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  first.unmount()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ enabled: true, authenticated: false })))
  const second = render(<AdminEntry />)
  expect(await screen.findByRole('link', { name: '登录' })).toHaveAttribute('href', '/admin/login')
  second.unmount()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ enabled: true, authenticated: true })))
  render(<AdminEntry />)
  expect(await screen.findByRole('link', { name: '控制台' })).toHaveAttribute('href', '/admin/leaderboards')
})

it('allows password manager input, prevents duplicate login and reports failure', async () => {
  window.history.replaceState({}, '', '/admin/login')
  let finish!: (response: Response) => void
  const fetcher = vi.fn((_path: RequestInfo | URL, init?: RequestInit) => init?.method === 'POST' ? new Promise<Response>((resolve) => { finish = resolve }) : Promise.resolve(json({ enabled: true, authenticated: false })))
  vi.stubGlobal('fetch', fetcher)
  const user = userEvent.setup()
  render(<AdminApp />)
  const username = await screen.findByLabelText('用户名')
  const password = screen.getByLabelText('密码')
  expect(username).toHaveAttribute('autocomplete', 'username')
  expect(password).toHaveAttribute('autocomplete', 'current-password')
  await user.type(username, 'owner')
  await user.type(password, 'incorrect-test-password')
  await user.click(screen.getByRole('button', { name: '登录' }))
  expect(screen.getByRole('button', { name: '正在登录…' })).toBeDisabled()
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  finish(json({}, 401))
  expect(await screen.findByRole('alert')).toHaveTextContent('用户名或密码错误')
  expect(screen.getByRole('button', { name: '登录' })).toBeEnabled()
})
