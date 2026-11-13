import { BrandMark } from './Brand'
import { LeaderboardTable } from './LeaderboardTable'
import { useLeaderboard } from '../hooks/useLeaderboard'
import type { Board, Period } from '../api/leaderboards'
import { boards, periods } from '../leaderboardLabels'

export function LeaderboardPanel({ board, period, selectionVersion = 0 }: { board: Board; period: Period; selectionVersion?: number }) {
  const { data, loading, error, avatars, retry } = useLeaderboard(board, period, selectionVersion)
  const previous = data && (loading || error || data.board !== board || data.period !== period)
  return <div className="board-content" aria-busy={loading}>
    <div className="board-message" role="status" aria-live="polite" aria-atomic="true">
      {loading && <p>正在加载{periods[period]} · {boards[board].label}…{previous && ' 以下保留上次结果。'}</p>}
      {error && <p>{error}{data && ' 以下保留上次结果。'}</p>}
    </div>
    {error && <button className="retry-button" type="button" onClick={retry}>重试榜单</button>}
    {!data && loading && <div className="ranking-empty"><span className="empty-label">LOADING</span><h2>正在加载真实榜单</h2></div>}
    {!data && error && <div className="ranking-empty"><div className="empty-symbol"><BrandMark /></div><h2>暂时无法加载榜单</h2><p>请检查连接后重试。</p></div>}
    {data && <>
      <div className="board-heading">
        <div><h2>{periods[data.period]} · {boards[data.board].label}{previous && <span className="previous-label">上次结果</span>}</h2><p>UTC {data.start} 至 {data.end}（不含结束日）</p></div>
        <p>缓存生成于 {data.generated_at.slice(0, 19).replace('T', ' ')} UTC</p>
      </div>
      {data.entries.length === 0 ? <div className="ranking-empty">
        <div className="empty-symbol" aria-hidden="true"><BrandMark /><span className="empty-symbol-dot" /></div>
        <span className="empty-label">WAITING FOR DATA</span><h2>等待第一份真实数据</h2><p>该榜单在此周期暂无符合条件的已接纳记录。<br />不使用模拟名次或虚构统计。</p>
      </div> : <LeaderboardTable key={`${data.board}/${data.period}`} data={data} avatars={avatars} />}
      <div className="ranking-bottom"><span>K 千 · M 百万 · B 十亿</span><span>已显示 {data.entries.length} 位 · 最多 100 位</span></div>
    </>}
  </div>
}
