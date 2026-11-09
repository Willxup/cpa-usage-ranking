import { BrandMark } from './Brand'
import { useLeaderboard } from '../hooks/useLeaderboard'
import type { AvatarCatalog, Board, Period } from '../api/leaderboards'
import { boards, periods } from '../leaderboardLabels'

function Avatar({ id, catalog }: { id: string; catalog: AvatarCatalog | null }) {
  const src = catalog?.paths.get(id) ?? catalog?.defaultPath
  return <span className="avatar" aria-hidden="true"><BrandMark />{src && <img key={src} src={src} alt="" width="44" height="44" onError={(event) => {
    const image = event.currentTarget
    if (catalog && image.getAttribute('src') !== catalog.defaultPath && !image.dataset.fallback) {
      image.dataset.fallback = 'true'
      image.src = catalog.defaultPath
    } else image.hidden = true
  }} />}</span>
}

function metric(value: string, board: Board) {
  return board === 'usage' ? BigInt(value).toLocaleString('en-US') : value
}

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
      </div> : <table className="leaderboard-table">
        <thead><tr><th scope="col">排名</th><th scope="col">参与者</th><th scope="col">{boards[data.board].metric}</th></tr></thead>
        <tbody>{data.entries.map((entry) => <tr key={entry.client_id}><td><span className="rank-number">{entry.rank}</span></td><td><div className="participant"><Avatar id={entry.avatar_id} catalog={avatars} /><span>{entry.display_name}</span></div></td><td className="metric-value">{metric(entry.metric, data.board)} <span>{boards[data.board].unit}</span></td></tr>)}</tbody>
      </table>}
      <div className="ranking-bottom"><span><i />显示真实缓存结果</span><span>最多展示 100 条</span></div>
    </>}
  </div>
}
