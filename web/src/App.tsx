import { useState } from 'react'
import { AdminEntry } from './admin/AdminEntry'
import type { Board, Period } from './api/leaderboards'
import { boards, periods } from './leaderboardLabels'
import { LeaderboardPanel } from './components/LeaderboardPanel'
import './styles/app.css'

export default function App({ active = true }: { active?: boolean }) {
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const { board, period } = selection
  return (
    <div className="app-shell">
      <header className="header-wrap">
        <div className="site-header page-width">
          <a className="brand" href="#overview" aria-label="CPA Usage Ranking 首页"><img className="project-icon" src="/assets/brand/project-icon.svg" width="40" height="40" alt="" /><span>CPA Usage Ranking</span></a>
          <nav aria-label="页面导航"><a className="nav-current" href="#ranking">排行榜</a><AdminEntry /></nav>
        </div>
      </header>
      <main id="main" className="page-width">
        <section id="overview" className="overview" aria-labelledby="page-title">
          <div><p className="eyebrow">A CLEARER VIEW OF AI USAGE</p><h1 id="page-title">AI 用量排行榜</h1></div>
        </section>
        <section id="ranking" className="ranking-section" aria-label="排行榜">
          <div className="ranking-toolbar">
            <fieldset className="board-control"><legend className="sr-only">榜单类型</legend>{(Object.keys(boards) as Board[]).map((item) => <button key={item} type="button" aria-pressed={board === item} onClick={() => setSelection((current) => current.board === item ? current : { ...current, board: item, version: current.version + 1 })}>{boards[item].label}</button>)}</fieldset>
            <fieldset className="period-control"><legend className="sr-only">时间范围</legend>{(Object.keys(periods) as Period[]).map((item) => <button key={item} type="button" aria-pressed={period === item} onClick={() => setSelection((current) => current.period === item ? current : { ...current, period: item, version: current.version + 1 })}>{periods[item]}</button>)}</fieldset>
          </div>
          <LeaderboardPanel board={board} period={period} selectionVersion={selection.version * 2 + (active ? 1 : 0)} />
        </section>
      </main>
      <footer className="site-footer page-width"><span>CPA Usage Ranking</span><p>真实数据，不使用示例统计。</p></footer>
    </div>
  )
}
