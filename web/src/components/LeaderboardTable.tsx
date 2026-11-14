import type { ReactNode } from 'react'
import { BrandMark } from './Brand'
import type { AvatarCatalog, Board, Entry, Leaderboard, OverallDetails } from '../api/leaderboards'
import { boards, periods } from '../leaderboardLabels'
import { exactMetric, formatMetric, type MetricKind } from '../formatMetrics'

const columns: { key: keyof OverallDetails; label: string; kind: MetricKind }[] = [
  { key: 'total_tokens', label: 'Token', kind: 'count' },
  { key: 'request_count', label: '请求数', kind: 'count' },
  { key: 'success_rate', label: '实际成功率', kind: 'percent' },
  { key: 'cache_read_rate', label: '缓存复用率', kind: 'percent' },
  { key: 'ttft_mean_ms', label: '平均首字延迟', kind: 'duration' },
  { key: 'latency_mean_ms', label: '平均总延迟', kind: 'duration' },
]
const kinds: Record<Board, MetricKind> = { overall: 'points', usage: 'count', cache: 'percent', success: 'points', ttft: 'duration' }

export function Avatar({ id, catalog }: { id: string; catalog: AvatarCatalog | null }) {
  const src = catalog?.paths.get(id) ?? catalog?.defaultPath
  return <span className="avatar" aria-hidden="true"><BrandMark />{src && <img key={src} src={src} alt="" width="44" height="44" onError={(event) => {
    const image = event.currentTarget
    if (catalog && image.getAttribute('src') !== catalog.defaultPath && !image.dataset.fallback) {
      image.dataset.fallback = 'true'
      image.src = catalog.defaultPath
    } else image.hidden = true
  }} />}</span>
}

function Amount({ value, kind }: { value: string | null | undefined; kind: MetricKind }) {
  const text = formatMetric(value, kind)
  return <span className={`metric-number${value == null ? ' metric-missing' : ''}`} title={exactMetric(value, kind)}>
    {text.split(/(ms|[KMB%分dhms])/).filter(Boolean).map((part, index) => <span key={index} className={/^(ms|[KMB%分dhms])$/.test(part) ? 'metric-unit' : undefined}>{part}</span>)}
  </span>
}

export function LeaderboardTable({ data, avatars, renderAction }: { data: Leaderboard; avatars: AvatarCatalog | null; renderAction?: (entry: Entry) => ReactNode }) {
  const overall = data.board === 'overall'
  return <table className={`leaderboard-table leaderboard-table--${data.board}${renderAction ? ' leaderboard-table--managed' : ''}`} role="table">
    <caption className="table-caption">{periods[data.period]} · {boards[data.board].label}</caption>
    <colgroup><col className="rank-column" /><col className="participant-column" /><col className="primary-column" />{overall && columns.map(({ key }) => <col key={key} />)}{renderAction && <col className="admin-action-column" />}</colgroup>
    <thead role="rowgroup"><tr role="row">
      <th scope="col" role="columnheader">排名</th><th scope="col" role="columnheader">参与者</th><th scope="col" role="columnheader" className="numeric-heading">{boards[data.board].metric}</th>
      {overall && columns.map(({ key, label }) => <th key={key} scope="col" role="columnheader" className="numeric-heading">{label}</th>)}
      {renderAction && <th scope="col" role="columnheader">操作</th>}
    </tr></thead>
    {data.entries.map((entry) => {
      return <tbody key={entry.client_id} role="rowgroup" className="leaderboard-group">
        <tr role="row" className="leaderboard-row">
          <td role="cell" className="rank-cell"><span className={`rank-number${entry.rank <= 3 ? ' rank-leading' : ''}`}>{entry.rank}</span></td>
          <td role="cell" className="participant-cell"><div className="participant"><Avatar id={entry.avatar_id} catalog={avatars} /><span id={`participant-${entry.client_id}`}>{entry.display_name}</span></div></td>
          <td role="cell" className="metric-value" data-label={boards[data.board].metric}><Amount value={entry.metric} kind={kinds[data.board]} /></td>
          {overall && columns.map(({ key, label, kind }) => <td key={key} role="cell" className="detail-cell" data-label={label}><Amount value={entry.details?.[key]} kind={kind} /></td>)}
          {renderAction && <td role="cell" className="admin-row-action">{renderAction(entry)}</td>}
        </tr>
      </tbody>
    })}
  </table>
}
