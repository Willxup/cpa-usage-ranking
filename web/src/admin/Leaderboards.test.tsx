import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import { Leaderboards } from './Leaderboards'

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('places a compact refresh action before the cache time in the same header', async () => {
  vi.stubGlobal('fetch', vi.fn((path: RequestInfo | URL) => Promise.resolve(json(String(path).includes('avatars') ? { default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] } : { board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T01:00:00Z', entries: [] }))))
  render(<Leaderboards session={{ enabled: true, authenticated: true }} onExpired={vi.fn()} />)
  const button = await screen.findByRole('button', { name: 'Refresh' })
  const time = screen.getByText('Cached at 2026-11-16 01:00:00 UTC')
  expect(button).toHaveClass('admin-refresh')
  expect(button.parentElement).toBe(time.parentElement)
  expect(button.compareDocumentPosition(time) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
  expect(button.closest('.board-heading')).not.toBeNull()
})

it('keeps the matching board and its disabled action until a requested board arrives', async () => {
  let finish!: (response: Response) => void
  const row = { client_id: '1', display_name: 'Old Participant', avatar_id: '1', rank: 1, metric: '90.00' }
  const base = { board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: '2026-11-16T01:00:00Z', entries: [row] }
  vi.stubGlobal('fetch', vi.fn((path: RequestInfo | URL) => {
    if (String(path).includes('avatars')) return Promise.resolve(json({}))
    if (String(path).includes('/usage/')) return new Promise<Response>((resolve) => { finish = resolve })
    return Promise.resolve(json(base))
  }))
  const events = userEvent.setup()
  render(<Leaderboards session={{ enabled: true, authenticated: true }} onExpired={vi.fn()} />)
  await screen.findByRole('row', { name: /Old Participant/ })
  await events.click(screen.getByRole('button', { name: 'Token Usage' }))
  await waitFor(() => expect(finish).toBeDefined())
  expect(screen.getByRole('heading', { name: 'Today · Overall' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Overall' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: 'Exclude' })).toBeDisabled()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  expect(screen.queryByText(/Loading|Previous result/)).not.toBeInTheDocument()
  await act(async () => finish(json({ ...base, board: 'usage', entries: [{ ...row, display_name: 'New Participant', metric: '1234' }] })))
  expect(await screen.findByRole('heading', { name: 'Today · Token Usage' })).toBeVisible()
  expect(screen.getByRole('row', { name: /New Participant/ })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Exclude' })).toBeEnabled()
})

it('confirms global exclusion and replaces the whole table with published server ranks and scores', async () => {
  let hidden = false
  const fetcher = vi.fn((path: RequestInfo | URL, init?: RequestInit) => {
    if (String(path).includes('ranking-visibility') && init?.method === 'PUT') { hidden = true; return Promise.resolve(json({ hidden: true })) }
    if (String(path).includes('avatars')) return Promise.resolve(json({ default_avatar_id: 1, avatars: [{ avatar_id: 1, path: 'images/001.webp' }] }))
    return Promise.resolve(json({ board: 'overall', period: 'today', start: '2026-11-16', end: '2026-11-17', generated_at: hidden ? '2026-11-16T01:00:01Z' : '2026-11-16T01:00:00Z', entries: hidden ? [{ client_id: '2', display_name: 'other', avatar_id: '1', rank: 1, metric: '95.00' }] : [{ client_id: '1', display_name: 'target', avatar_id: '1', rank: 1, metric: '90.00' }, { client_id: '2', display_name: 'other', avatar_id: '1', rank: 2, metric: '80.00' }] }))
  })
  vi.stubGlobal('fetch', fetcher)
  const events = userEvent.setup()
  render(<Leaderboards session={{ enabled: true, authenticated: true, csrf_token: 'csrf' }} onExpired={vi.fn()} />)
  let row = await screen.findByRole('row', { name: /target/ })
  expect(within(row).getByRole('button', { name: 'Exclude' })).toHaveClass('admin-button--moderation')
  expect(screen.getByRole('columnheader', { name: 'Action' })).toHaveClass('admin-row-action')
  expect(within(row).getByRole('button', { name: 'Exclude' }).closest('td')).toHaveClass('admin-row-action')
  expect(screen.queryByRole('button', { name: /Exclude all|全部下榜/ })).not.toBeInTheDocument()
  const trigger = within(row).getByRole('button', { name: 'Exclude' })
  await events.click(trigger)
  expect(screen.getByRole('dialog')).toHaveTextContent('Removes this client from all leaderboards while keeping data and accepting future reports.')
  await events.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(trigger).toHaveFocus()
  expect(fetcher.mock.calls.some(([path]) => String(path).includes('ranking-visibility'))).toBe(false)
  await events.click(trigger)
  await events.click(screen.getByRole('button', { name: 'Confirm' }))
  expect(await screen.findByTitle('95.00 pts')).toBeVisible()
  expect(screen.queryByRole('row', { name: /target/ })).not.toBeInTheDocument()
  expect(document.querySelector('.admin-notice')).toBeNull()
  row = screen.getByRole('row', { name: /other/ })
  expect(within(row).getByText('1', { exact: true })).toBeVisible()
  act(() => setLocale('zh'))
  expect(await screen.findByTitle('95.00 分')).toBeVisible()
  const write = fetcher.mock.calls.find(([path]) => String(path).includes('ranking-visibility'))
  expect(JSON.parse(String(write?.[1]?.body))).toEqual({ hidden: true })
})
