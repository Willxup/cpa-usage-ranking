import { useState } from 'react'
import type { Board, Entry, Period } from '../api/leaderboards'
import { LeaderboardPanel } from '../components/LeaderboardPanel'
import { boards, periods } from '../leaderboardLabels'
import { AdminError, adminRequest, jsonRequest, type AdminSession } from './api'
import { waitForExclusion } from './refreshLeaderboard'
import { Users } from './Users'

export function Leaderboards({ session, onExpired }: { session: AdminSession; onExpired: () => void }) {
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const [hiddenView, setHiddenView] = useState(false)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const { board, period } = selection
  async function hide(entry: Entry, generatedAt: string) {
    if (busy || !window.confirm(`下榜“${entry.display_name}”（ID ${entry.client_id}）？退出全部榜单，保留数据且继续接收上报，直到手动恢复。`)) return
    setBusy(entry.client_id); setNotice(''); setError('')
    try {
      await adminRequest(`users/${entry.client_id}/ranking-visibility`, jsonRequest('PUT', { hidden: true }, session.csrf_token))
      setNotice('设置已保存，榜单刷新后生效。可在“已下榜用户”中手动恢复。')
      const published = await waitForExclusion(board, period, generatedAt, entry.client_id)
      setSelection((old) => ({ ...old, version: old.version + 1 }))
      if (!published) setNotice('设置已保存，榜单刷新后生效。暂未读取到新榜单，可点击“刷新榜单”重试；恢复入口在“已下榜用户”。')
    } catch (error) { setError((error as Error).message); if (error instanceof AdminError && error.status === 401) onExpired() }
    finally { setBusy('') }
  }
  return <section aria-label="榜单管理">
    <div className="admin-heading"><div><p className="eyebrow">LEADERBOARDS</p><h1>榜单管理</h1></div></div>
    <fieldset className="board-control admin-view-control"><legend className="sr-only">管理视图</legend><button type="button" aria-pressed={!hiddenView} onClick={() => setHiddenView(false)}>当前榜单</button><button type="button" aria-pressed={hiddenView} onClick={() => setHiddenView(true)}>已下榜用户</button></fieldset>
    {notice && <p className="admin-notice" role="status">{notice}</p>}
    {error && <p className="admin-error" role="alert">{error}</p>}
    {hiddenView ? <Users session={session} hiddenOnly onExpired={onExpired} /> : <>
      <div className="ranking-toolbar">
        <fieldset className="board-control"><legend className="sr-only">榜单类型</legend>{(Object.keys(boards) as Board[]).map((item) => <button key={item} type="button" aria-pressed={board === item} onClick={() => setSelection((old) => ({ ...old, board: item, version: old.version + 1 }))}>{boards[item].label}</button>)}</fieldset>
        <fieldset className="period-control"><legend className="sr-only">时间范围</legend>{(Object.keys(periods) as Period[]).map((item) => <button key={item} type="button" aria-pressed={period === item} onClick={() => setSelection((old) => ({ ...old, period: item, version: old.version + 1 }))}>{periods[item]}</button>)}</fieldset>
      </div>
      <LeaderboardPanel compactLoading board={board} period={period} selectionVersion={selection.version} headingAction={<button className="admin-button admin-refresh" type="button" disabled={!!busy} onClick={() => setSelection((old) => ({ ...old, version: old.version + 1 }))}>刷新榜单</button>} renderAction={(entry, generatedAt) => <button className="admin-button admin-button--danger admin-button--moderation" type="button" disabled={!!busy} onClick={() => void hide(entry, generatedAt)}>{busy === entry.client_id ? '正在保存…' : '下榜'}</button>} />
    </>}
  </section>
}
