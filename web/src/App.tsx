import { useEffect, useState } from 'react'
import type { Board, Period } from './api/leaderboards'
import { BOARD_ORDER, PERIOD_ORDER } from './leaderboardLabels'
import { LeaderboardView } from './components/LeaderboardPanel'
import { SiteFooter } from './components/SiteFooter'
import { SiteHeader } from './components/SiteHeader'
import { useLeaderboard } from './hooks/useLeaderboard'
import { useI18n } from './i18n'
import './styles/app.css'

export default function App({ active = true }: { active?: boolean }) {
  const { m } = useI18n()
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const { board, period } = selection
  const result = useLeaderboard(board, period, selection.version, active)
  const shown = result.data ?? selection
  useEffect(() => {
    if (!active) return
    return () => setSelection((old) => ({ ...old, version: old.version + 1 }))
  }, [active])
  function select(change: Partial<{ board: Board; period: Period }>) {
    setSelection((current) => {
      const base = result.error && result.data ? result.data : current
      const next = { board: change.board ?? base.board, period: change.period ?? base.period }
      return !result.error && next.board === current.board && next.period === current.period ? current : { ...next, version: current.version + 1 }
    })
  }
  return (
    <div className="app-shell">
      <SiteHeader />
      <main id="main" className="page-width">
        <section id="overview" className="overview" aria-labelledby="page-title">
          <div><p className="eyebrow">{m.publicPage.eyebrow}</p><h1 id="page-title">{m.publicPage.title}</h1></div>
        </section>
        <section id="ranking" className="ranking-section" aria-label={m.publicPage.sectionAria}>
          <div className="ranking-toolbar">
            <fieldset className="board-control"><legend className="sr-only">{m.publicPage.boardLegend}</legend>{BOARD_ORDER.map((item) => <button key={item} type="button" aria-pressed={shown.board === item} onClick={() => select({ board: item })}>{m.boards[item].label}</button>)}</fieldset>
            <fieldset className="period-control"><legend className="sr-only">{m.publicPage.periodLegend}</legend>{PERIOD_ORDER.map((item) => <button key={item} type="button" aria-pressed={shown.period === item} onClick={() => select({ period: item })}>{m.periods[item]}</button>)}</fieldset>
          </div>
          <LeaderboardView board={board} period={period} result={result} quietSwitch />
        </section>
      </main>
      <SiteFooter />
    </div>
  )
}
