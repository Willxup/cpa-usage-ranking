import { useState } from 'react'
import type { Board, Period } from './api/leaderboards'
import { boards, periods } from './leaderboardLabels'
import { LeaderboardView } from './components/LeaderboardPanel'
import { SiteFooter } from './components/SiteFooter'
import { SiteHeader } from './components/SiteHeader'
import { useLeaderboard } from './hooks/useLeaderboard'
import './styles/app.css'

export default function App({ active = true }: { active?: boolean }) {
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const { board, period } = selection
  const result = useLeaderboard(board, period, selection.version * 2 + (active ? 1 : 0))
  const shown = result.data ?? selection
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
          <div><p className="eyebrow">A CLEARER VIEW OF AI USAGE</p><h1 id="page-title">AI 用量排行榜</h1></div>
        </section>
        <section id="ranking" className="ranking-section" aria-label="排行榜">
          <div className="ranking-toolbar">
            <fieldset className="board-control"><legend className="sr-only">榜单类型</legend>{(Object.keys(boards) as Board[]).map((item) => <button key={item} type="button" aria-pressed={shown.board === item} onClick={() => select({ board: item })}>{boards[item].label}</button>)}</fieldset>
            <fieldset className="period-control"><legend className="sr-only">时间范围</legend>{(Object.keys(periods) as Period[]).map((item) => <button key={item} type="button" aria-pressed={shown.period === item} onClick={() => select({ period: item })}>{periods[item]}</button>)}</fieldset>
          </div>
          <LeaderboardView board={board} period={period} result={result} quietSwitch />
        </section>
      </main>
      <SiteFooter />
    </div>
  )
}
