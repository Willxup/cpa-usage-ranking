import type { Entry } from '../api/leaderboards'

export function OverallMetrics({ entry }: { entry: Entry }) {
  const d = entry.details
  const values = [
    { label: 'Token', value: d?.total_tokens, kind: 'count' },
    { label: '请求数', value: d?.request_count, kind: 'count' },
    { label: '实际成功率', value: d?.success_rate, kind: 'percent' },
    { label: '缓存复用率', value: d?.cache_read_rate, kind: 'percent' },
    { label: '平均首字延迟', value: d?.ttft_mean_ms, kind: 'mean' },
    { label: '平均总延迟', value: d?.latency_mean_ms, kind: 'mean' },
  ]
  return <dl className="overall-details" aria-label="综合榜详细数据" aria-describedby={`participant-${entry.client_id}`}>
    {values.map(({ label, value, kind }) => <div key={label} className={`overall-detail overall-detail--${kind}`}>
      <dt>{label}</dt><dd>{value == null ? '—' : kind === 'count' ? BigInt(value).toLocaleString('en-US') : <>{value}<span className="metric-unit">{kind === 'percent' ? '%' : ' ms'}</span></>}</dd>
    </div>)}
  </dl>
}
