import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { Users } from './Users'

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const user = { id: '9007199254740993', display_name: '<img src=x>', avatar_id: '1', created_at: '2026-11-16T00:00:00Z', disabled: true }
afterEach(() => vi.unstubAllGlobals())

it('renders names as text, preserves ID precision and resets paging on query', async () => {
  const fetcher = vi.fn((path: RequestInfo | URL) => Promise.resolve(json(String(path).includes('avatars') ? { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] } : { users: [user], next_cursor: String(path).includes('after=') ? '' : user.id })))
  vi.stubGlobal('fetch', fetcher)
  const events = userEvent.setup()
  render(<Users onExpired={vi.fn()} />)
  expect(await screen.findByText(user.id)).toBeVisible()
  expect(screen.getByText('<img src=x>')).toBeVisible()
  expect(document.querySelector('img[src=x]')).toBeNull()
  expect(screen.queryByRole('button', { name: /封禁|解封/ })).not.toBeInTheDocument()
  await events.click(screen.getByRole('button', { name: '下一页' }))
  expect(await screen.findByText('第 2 页 · 1 位用户')).toBeVisible()
  await events.type(screen.getByLabelText('查询内容'), 'literal%')
  await events.click(screen.getByRole('button', { name: '查询' }))
  expect(await screen.findByText('第 1 页 · 1 位用户')).toBeVisible()
  expect(fetcher.mock.calls.some(([path]) => String(path) === '/v1/admin/users?limit=50&name=literal%25')).toBe(true)
})

it('distinguishes load failure, retry and an empty list', async () => {
  let failed = false
  vi.stubGlobal('fetch', vi.fn((path: RequestInfo | URL) => {
    if (String(path).includes('avatars')) return Promise.resolve(json({}, 404))
    if (!failed) { failed = true; return Promise.resolve(json({}, 503)) }
    return Promise.resolve(json({ users: [], next_cursor: '' }))
  }))
  const events = userEvent.setup()
  render(<Users onExpired={vi.fn()} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用')
  await events.click(screen.getByRole('button', { name: '重试用户列表' }))
  expect(await screen.findByText('未找到匹配的用户。')).toBeVisible()
})
