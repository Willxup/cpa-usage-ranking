import { useEffect, useState } from 'react'
import type { Board, Entry, Period } from '../api/leaderboards'
import { LeaderboardView } from '../components/LeaderboardPanel'
import { useLeaderboard } from '../hooks/useLeaderboard'
import { useI18n } from '../i18n'
import { BOARD_ORDER, PERIOD_ORDER } from '../leaderboardLabels'
import { AdminError, adminRequest, jsonRequest, type AdminSession } from './api'
import { waitForExclusion } from './refreshLeaderboard'
import { Users } from './Users'
import { ConfirmDialog } from './ConfirmDialog'

export function Leaderboards({ session, onExpired, active = true }: { session: AdminSession; onExpired: () => void; active?: boolean }) {
  const { m } = useI18n()
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const [hiddenView, setHiddenView] = useState(false)
  const [busy, setBusy] = useState('')
  const [target, setTarget] = useState<{ entry: Entry; generatedAt: string } | null>(null)
  const { board, period } = selection
  const result = useLeaderboard(board, period, selection.version, active && !hiddenView)
  const shown = result.data ?? selection
  useEffect(() => {
    if (!active || hiddenView) return
    return () => { setTarget(null); setSelection((old) => ({ ...old, version: old.version + 1 })) }
  }, [active, hiddenView])
  function select(change: Partial<{ board: Board; period: Period }>) {
    setSelection((current) => {
      const base = result.error && result.data ? result.data : current
      const next = { board: change.board ?? base.board, period: change.period ?? base.period }
      return !result.error && next.board === current.board && next.period === current.period ? current : { ...next, version: current.version + 1 }
    })
  }
  async function hide(entry: Entry, generatedAt: string) {
    if (busy) return
    setBusy(entry.client_id)
    try {
      await adminRequest(`users/${entry.client_id}/ranking-visibility`, jsonRequest('PUT', { hidden: true }, session.csrf_token))
      void waitForExclusion(board, period, generatedAt, entry.client_id).then(() => {
        setSelection((old) => ({ ...old, version: old.version + 1 })); setBusy('')
      })
    } catch (error) { setBusy(''); if (error instanceof AdminError && error.status === 401) onExpired(); throw error }
  }
  return <section aria-label={m.admin.boardsRegion}>
    <div className="admin-heading"><div><p className="eyebrow">{m.admin.boardsEyebrow}</p><h1>{m.admin.boardsTitle}</h1></div></div>
    <fieldset className="board-control admin-view-control"><legend className="sr-only">{m.admin.viewLegend}</legend><button type="button" aria-pressed={!hiddenView} onClick={() => setHiddenView(false)}>{m.admin.activeBoardTab}</button><button type="button" aria-pressed={hiddenView} onClick={() => setHiddenView(true)}>{m.admin.excludedUsersTab}</button></fieldset>
    <div className="tab-view" hidden={hiddenView}>
      <div className="ranking-toolbar">
        <fieldset className="board-control"><legend className="sr-only">{m.publicPage.boardLegend}</legend>{BOARD_ORDER.map((item) => <button key={item} type="button" aria-pressed={shown.board === item} onClick={() => select({ board: item })}>{m.boards[item].label}</button>)}</fieldset>
        <fieldset className="period-control"><legend className="sr-only">{m.publicPage.periodLegend}</legend>{PERIOD_ORDER.map((item) => <button key={item} type="button" aria-pressed={shown.period === item} onClick={() => select({ period: item })}>{m.periods[item]}</button>)}</fieldset>
      </div>
      <LeaderboardView quietSwitch board={board} period={period} result={result} headingAction={<button className="admin-button admin-refresh" type="button" disabled={!!busy} onClick={() => setSelection((old) => ({ ...old, version: old.version + 1 }))}>{m.admin.refreshButton}</button>} renderAction={(entry, generatedAt, stale) => <button className="admin-button admin-button--danger admin-button--moderation" type="button" disabled={!!busy || stale} onClick={() => setTarget({ entry, generatedAt })}>{busy === entry.client_id ? m.admin.excludingBusy : m.admin.excludeButton}</button>} />
    </div>
    <div className="tab-view" hidden={!hiddenView}><Users session={session} hiddenOnly active={active && hiddenView} onExpired={onExpired} /></div>
    {target && <ConfirmDialog title={m.admin.excludeConfirmTitle(target.entry.display_name)} detail={m.admin.excludeConfirmDetail} onCancel={() => setTarget(null)} onConfirm={() => hide(target.entry, target.generatedAt)} />}
  </section>
}
