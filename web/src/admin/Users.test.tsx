import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { Users } from './Users'

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const user = { id: '9007199254740993', display_name: '<img src=x>', avatar_id: '1', created_at: '2026-11-16T00:00:00Z', disabled: true, ranking_hidden: true }
afterEach(() => vi.unstubAllGlobals())

it('renders names as text, preserves ID precision and resets paging on query', async () => {
  const fetcher = vi.fn((path: RequestInfo | URL) => Promise.resolve(json(String(path).includes('avatars') ? { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] } : { users: [user], next_cursor: String(path).includes('after=') ? '' : user.id })))
  vi.stubGlobal('fetch', fetcher)
  const events = userEvent.setup()
  render(<Users session={{ enabled: true, authenticated: true, csrf_token: 'csrf' }} onExpired={vi.fn()} />)
  expect(await screen.findByText(user.id)).toBeVisible()
  expect(screen.getByText('<img src=x>')).toBeVisible()
  expect(document.querySelector('img[src=x]')).toBeNull()
  expect(screen.getByRole('button', { name: '解封' })).toBeEnabled()
  expect(screen.getByRole('button', { name: '解封' })).toHaveClass('admin-button--moderation')
  await events.click(screen.getByRole('button', { name: '下一页' }))
  expect(await screen.findByText('第 2 页 · 1 位用户')).toBeVisible()
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  expect(screen.getAllByRole('searchbox')).toHaveLength(1)
  await events.type(screen.getByLabelText('昵称或ID'), 'literal%')
  await events.click(screen.getByRole('button', { name: '查询' }))
  expect(await screen.findByText('第 1 页 · 1 位用户')).toBeVisible()
  expect(fetcher.mock.calls.some(([path]) => String(path) === '/v1/admin/users?limit=50&q=literal%25')).toBe(true)
})

it('sends an explicit target state once and reports failed writes without changing the row', async () => {
  let finish!: (response: Response) => void
  const fetcher = vi.fn((path: RequestInfo | URL, init?: RequestInit) => init?.method === 'PUT' ? new Promise<Response>((resolve) => { finish = resolve }) : Promise.resolve(json(String(path).includes('avatars') ? {} : { users: [user], next_cursor: '' })))
  vi.stubGlobal('fetch', fetcher)
  const events = userEvent.setup()
  render(<Users session={{ enabled: true, authenticated: true, csrf_token: 'csrf' }} onExpired={vi.fn()} />)
  await events.click(await screen.findByRole('button', { name: '解封' }))
  expect(screen.getByRole('dialog')).toHaveTextContent('解封')
  await events.click(screen.getByRole('button', { name: '确认' }))
  expect(screen.getByRole('button', { name: '正在保存…' })).toBeDisabled()
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === 'PUT')
  expect(writes).toHaveLength(1)
  expect(JSON.parse(String(writes[0][1]?.body))).toEqual({ disabled: false })
  finish(json({}, 503))
  expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用')
  await events.click(screen.getByRole('button', { name: '取消' }))
  expect(screen.getByRole('button', { name: '解封' })).toBeEnabled()
})

it('uses the global hidden filter and requires unbanning before restoration', async () => {
  const fetcher = vi.fn((path: RequestInfo | URL) => Promise.resolve(json(String(path).includes('avatars') ? {} : { users: [user], next_cursor: '' })))
  vi.stubGlobal('fetch', fetcher)
  render(<Users session={{ enabled: true, authenticated: true }} hiddenOnly onExpired={vi.fn()} />)
  expect(await screen.findByRole('button', { name: '上榜' })).toBeDisabled()
  expect(screen.getByText('先解封，再恢复')).toBeVisible()
  expect(screen.getByRole('link', { name: '前往用户管理' })).toHaveAttribute('href', '/admin/users')
  expect(screen.queryByLabelText('下榜状态')).not.toBeInTheDocument()
  expect(fetcher.mock.calls.some(([path]) => String(path) === '/v1/admin/users?limit=50&hidden=true')).toBe(true)
})

it('restores eligibility with an explicit false state and refreshes the user list', async () => {
  let restored = false
  const fetcher = vi.fn((path: RequestInfo | URL, init?: RequestInit) => {
    if (String(path).includes('avatars')) return Promise.resolve(json({}))
    if (init?.method === 'PUT') { restored = true; return Promise.resolve(json({ hidden: false })) }
    return Promise.resolve(json({ users: restored ? [] : [{ ...user, disabled: false }], next_cursor: '' }))
  })
  vi.stubGlobal('fetch', fetcher)
  const events = userEvent.setup()
  render(<Users session={{ enabled: true, authenticated: true, csrf_token: 'csrf' }} hiddenOnly onExpired={vi.fn()} />)
  await events.click(await screen.findByRole('button', { name: '上榜' }))
  await events.click(screen.getByRole('button', { name: '确认' }))
  expect(await screen.findByText('未找到匹配的用户。')).toBeVisible()
  expect(document.querySelector('.admin-notice')).toBeNull()
  expect(fetcher.mock.calls.filter(([, init]) => init?.method === 'PUT').map(([, init]) => JSON.parse(String(init?.body)))).toEqual([{ hidden: false }])
})

it('distinguishes load failure, retry and an empty list', async () => {
  let failed = false
  vi.stubGlobal('fetch', vi.fn((path: RequestInfo | URL) => {
    if (String(path).includes('avatars')) return Promise.resolve(json({}, 404))
    if (!failed) { failed = true; return Promise.resolve(json({}, 503)) }
    return Promise.resolve(json({ users: [], next_cursor: '' }))
  }))
  const events = userEvent.setup()
  render(<Users session={{ enabled: true, authenticated: true }} onExpired={vi.fn()} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用')
  await events.click(screen.getByRole('button', { name: '重试用户列表' }))
  expect(await screen.findByText('未找到匹配的用户。')).toBeVisible()
})
