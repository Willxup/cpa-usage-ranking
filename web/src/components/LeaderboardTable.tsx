import type { ReactNode } from 'react'
import { BrandMark } from './Brand'
import type { AvatarCatalog, Board, Entry, Leaderboard, OverallDetails } from '../api/leaderboards'
import { exactMetric, formatMetric, type MetricKind } from '../formatMetrics'
import { useI18n, type Locale } from '../i18n'

const columns: { key: keyof OverallDetails; kind: MetricKind }[] = [
  { key: 'total_tokens', kind: 'count' },
  { key: 'request_count', kind: 'count' },
  { key: 'success_rate', kind: 'percent' },
  { key: 'cache_read_rate', kind: 'percent' },
  { key: 'ttft_mean_ms', kind: 'duration' },
  { key: 'latency_mean_ms', kind: 'duration' },
]
const kinds: Record<Board, MetricKind> = { overall: 'points', usage: 'count', cache: 'percent', success: 'points', ttft: 'duration' }
const unitSplitPattern = /(pts|ms|[KMB%分dhms])/
const unitTokenPattern = /^(pts|ms|[KMB%分dhms])$/

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

function Amount({ value, kind, locale }: { value: string | null | undefined; kind: MetricKind; locale: Locale }) {
  const text = formatMetric(value, kind, locale)
  return <span className={`metric-number${value == null ? ' metric-missing' : ''}`} title={exactMetric(value, kind, locale)}>
    {text.split(unitSplitPattern).filter(Boolean).map((part, index) => <span key={index} className={unitTokenPattern.test(part) ? 'metric-unit' : undefined}>{part}</span>)}
  </span>
}

export function LeaderboardTable({ data, avatars, renderAction }: { data: Leaderboard; avatars: AvatarCatalog | null; renderAction?: (entry: Entry) => ReactNode }) {
  const { locale, m } = useI18n()
  const overall = data.board === 'overall'
  return <table className={`leaderboard-table leaderboard-table--${data.board}${renderAction ? ' leaderboard-table--managed' : ''}`} role="table">
    <caption className="table-caption">{m.panel.heading(m.periods[data.period], m.boards[data.board].label)}</caption>
    <colgroup><col className="rank-column" /><col className="participant-column" /><col className="primary-column" />{overall && columns.map(({ key }) => <col key={key} />)}{renderAction && <col className="admin-action-column" />}</colgroup>
    <thead role="rowgroup"><tr role="row">
      <th scope="col" role="columnheader">{m.table.rank}</th><th scope="col" role="columnheader">{m.table.participant}</th><th scope="col" role="columnheader" className="numeric-heading">{m.boards[data.board].metric}</th>
      {overall && columns.map(({ key }) => <th key={key} scope="col" role="columnheader" className="numeric-heading">{m.table.details[key]}</th>)}
      {renderAction && <th scope="col" role="columnheader" className="admin-row-action">{m.table.action}</th>}
    </tr></thead>
    {data.entries.map((entry) => {
      return <tbody key={entry.client_id} role="rowgroup" className="leaderboard-group">
        <tr role="row" className="leaderboard-row">
          <td role="cell" className="rank-cell"><span className={`rank-number${entry.rank <= 3 ? ` rank-leading rank-${entry.rank}` : ''}`}>{entry.rank}</span></td>
          <td role="cell" className="participant-cell"><div className="participant"><Avatar id={entry.avatar_id} catalog={avatars} /><span id={`${renderAction ? 'admin-' : ''}participant-${entry.client_id}`}>{entry.display_name}</span></div></td>
          <td role="cell" className="metric-value" data-label={m.boards[data.board].metric}><Amount value={entry.metric} kind={kinds[data.board]} locale={locale} /></td>
          {overall && columns.map(({ key, kind }) => <td key={key} role="cell" className="detail-cell" data-label={m.table.details[key]}><Amount value={entry.details?.[key]} kind={kind} locale={locale} /></td>)}
          {renderAction && <td role="cell" className="admin-row-action">{renderAction(entry)}</td>}
        </tr>
      </tbody>
    })}
  </table>
}
