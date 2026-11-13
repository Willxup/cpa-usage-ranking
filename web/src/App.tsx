import { useState } from 'react'
import type { Board, Period } from './api/leaderboards'
import { boards, periods } from './leaderboardLabels'
import { LeaderboardPanel } from './components/LeaderboardPanel'
import { StatusPanel } from './components/StatusPanel'
import { useHealth } from './hooks/useHealth'
import './styles/app.css'

export default function App() {
  const health = useHealth()
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const { board, period } = selection
  return (
    <div className="app-shell">
      <header className="header-wrap">
        <div className="site-header page-width">
          <a className="brand" href="#overview" aria-label="CPA Usage Ranking 首页"><img className="project-icon" src="/assets/brand/project-icon.svg" width="40" height="40" alt="" /><span>CPA Usage Ranking</span></a>
          <nav aria-label="页面导航"><a className="nav-current" href="#ranking">排行榜</a><a href="#foundation">关于</a></nav>
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
          <LeaderboardPanel board={board} period={period} selectionVersion={selection.version} />
        </section>
        <section id="foundation" className="foundation" aria-labelledby="foundation-heading">
          <div className="foundation-copy"><span className="eyebrow">ABOUT THE RANKING</span><h2 id="foundation-heading">真实用量，清晰呈现。</h2><p>榜单来自已接纳的使用记录，按照北京时间周期汇总。<br />排行不代表受控性能认证，也不验证自报数据的真实性。</p></div>
          <StatusPanel state={health.state} message={health.message} onCheck={health.check} />
        </section>
      </main>
      <footer className="site-footer page-width"><span>CPA Usage Ranking</span><p>真实数据，不使用示例统计。</p></footer>
    </div>
  )
}
