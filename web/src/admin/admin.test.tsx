import { screen, within } from '@testing-library/react'
import { renderWithSession as render } from '../test/renderWithSession'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { AdminEntry } from './AdminEntry'
import AdminApp from './AdminApp'
import { Accounts } from './Accounts'

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

it('ordinary administrator cannot render account management or fetch its data', async () => {
  window.history.replaceState({}, '', '/admin/accounts')
  const fetcher = vi.fn().mockResolvedValue(json({ enabled: true, authenticated: true, user: { id: '17', username: 'staff', owner: false } }))
  vi.stubGlobal('fetch', fetcher)
  render(<AdminApp />)
  expect(await screen.findByRole('heading', { name: '无访问权限' })).toBeVisible()
  expect(screen.queryByRole('link', { name: '管理员管理' })).not.toBeInTheDocument()
  expect(fetcher.mock.calls.every(([path]) => path === '/v1/admin/session')).toBe(true)
})

it('orders the add form before a separately named administrator list', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ owner: { username: 'owner' }, accounts: [{ id: '2', username: 'demo-iris', created_at: '2026-11-16T00:00:00Z' }] })))
  render(<Accounts session={{ enabled: true, authenticated: true }} onExpired={vi.fn()} />)
  const add = screen.getByRole('region', { name: '添加管理员' })
  const list = screen.getByRole('region', { name: '管理员列表' })
  expect(add.parentElement).toBe(list.parentElement)
  expect(add.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
  expect(within(add).getByLabelText('初始密码')).toBeVisible()
  expect(await within(list).findByText('demo-iris')).toBeVisible()
})

it('shows one administrator identity without role/config explanations and retains input validation', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ owner: { username: 'configuration-account' }, accounts: [{ id: '2', username: 'reviewer', created_at: '2026-11-16T00:00:00Z' }] })))
  render(<Accounts session={{ enabled: true, authenticated: true }} onExpired={vi.fn()} />)
  expect(await screen.findByText('reviewer')).toBeVisible()
  expect(screen.queryByText('configuration-account')).not.toBeInTheDocument()
  expect(screen.queryByText(/主管理员|普通管理员|配置文件|只读|用户名 3–64/)).not.toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '添加管理员' })).toBeVisible()
  expect(screen.getByLabelText('用户名')).toHaveAttribute('minlength', '3')
  expect(screen.getByLabelText('初始密码')).toHaveAttribute('minlength', '12')
})

it('does not retry a failed account creation automatically', async () => {
  const fetcher = vi.fn((_path: RequestInfo | URL, init?: RequestInit) => Promise.resolve(init?.method === 'POST' ? json({}, 409) : json({ owner: { username: 'owner' }, accounts: [] })))
  vi.stubGlobal('fetch', fetcher)
  const user = userEvent.setup()
  render(<Accounts session={{ enabled: true, authenticated: true, csrf_token: 'test-csrf' }} onExpired={vi.fn()} />)
  expect(await screen.findByText('暂无管理员。')).toBeVisible()
  await user.type(screen.getByLabelText('用户名'), 'staff')
  await user.type(screen.getByLabelText('初始密码'), 'isolated-test-password')
  await user.click(screen.getByRole('button', { name: '添加管理员' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('用户名已存在')
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
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
