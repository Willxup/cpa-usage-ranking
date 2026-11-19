import type { ReactNode } from 'react'
import { BrandMark } from './Brand'
import { LeaderboardTable } from './LeaderboardTable'
import { useLeaderboard } from '../hooks/useLeaderboard'
import type { Board, Entry, Period } from '../api/leaderboards'
import { useI18n } from '../i18n'

type PanelProps = { board: Board; period: Period; selectionVersion?: number; renderAction?: (entry: Entry, generatedAt: string, stale: boolean) => ReactNode; compactLoading?: boolean; headingAction?: ReactNode }

export function LeaderboardPanel(props: PanelProps) {
  const result = useLeaderboard(props.board, props.period, props.selectionVersion ?? 0)
  return <LeaderboardView {...props} result={result} />
}

export function LeaderboardView({ board, period, renderAction, compactLoading = false, headingAction, quietSwitch = false, result: { data, loading, error, avatars, retry } }: PanelProps & { quietSwitch?: boolean; result: ReturnType<typeof useLeaderboard> }) {
  const { m } = useI18n()
  const previous = data && (loading || error || data.board !== board || data.period !== period)
  return <div className="board-content" aria-busy={loading}>
    <div className="board-message" role="status" aria-live="polite" aria-atomic="true">
      {loading && !compactLoading && !quietSwitch && <p>{m.panel.loadingStatus(m.periods[period], m.boards[board].label, !!previous)}</p>}
      {error && <p>{m.panel.errorWithPrevious(error, !!data)}</p>}
    </div>
    {error && <button className="retry-button" type="button" onClick={retry}>{m.panel.retryButton}</button>}
    {!data && loading && (quietSwitch || compactLoading ? <div className="ranking-empty" aria-hidden="true" /> : <div className="ranking-empty"><span className="empty-label">{m.panel.loadingBadge}</span><h2>{m.panel.loadingTitle}</h2></div>)}
    {!data && error && <div className="ranking-empty"><div className="empty-symbol"><BrandMark /></div><h2>{m.panel.errorTitle}</h2><p>{m.panel.errorDetail}</p></div>}
    {data && <div className="leaderboard-result" key={`${data.board}/${data.period}`}>
      <div className="board-heading">
        <div><h2>{m.panel.heading(m.periods[data.period], m.boards[data.board].label)}{previous && !(quietSwitch && loading) && <span className="previous-label">{m.panel.previousBadge}</span>}</h2><p>{m.panel.utcRange(data.start, data.end)}</p></div>
        {headingAction ? <div className="admin-board-generation">{headingAction}<p>{m.panel.cachedAt(data.generated_at.slice(0, 19).replace('T', ' '))}</p></div> : <p>{m.panel.cachedAt(data.generated_at.slice(0, 19).replace('T', ' '))}</p>}
      </div>
      {data.entries.length === 0 ? <div className="ranking-empty">
        <div className="empty-symbol" aria-hidden="true"><BrandMark /><span className="empty-symbol-dot" /></div>
        <span className="empty-label">{m.panel.emptyBadge}</span><h2>{m.panel.emptyTitle}</h2><p>{m.panel.emptyLine1}<br />{m.panel.emptyLine2}</p>
      </div> : <LeaderboardTable key={`${data.board}/${data.period}`} data={data} avatars={avatars} renderAction={renderAction ? (entry) => renderAction(entry, data.generated_at, !!previous) : undefined} />}
      <div className="ranking-bottom"><span>{m.panel.showingCount(data.entries.length)}</span></div>
    </div>}
  </div>
}
