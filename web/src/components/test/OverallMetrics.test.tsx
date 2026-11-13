import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { OverallMetrics } from '../OverallMetrics'
import type { Entry } from '../../api/leaderboards'

const entry: Entry = { rank: 1, client_id: '1', display_name: 'Alice', avatar_id: '1', metric: '51.16', details: { total_tokens: '9223372036854775807', request_count: '9007199254740993', success_rate: '75.00', cache_read_rate: '25.13', ttft_mean_ms: '10.33', latency_mean_ms: '200.50' } }

describe('overall metrics', () => {
  it('shows all six values with exact integers, explicit units and raw success labeling', () => {
    const { container } = render(<OverallMetrics entry={entry} />)
    expect([...container.querySelectorAll('dt')].map((e) => e.textContent)).toEqual(['Token', '请求数', '实际成功率', '缓存复用率', '平均首字延迟', '平均总延迟'])
    expect([...container.querySelectorAll('dd')].map((e) => e.textContent)).toEqual(['9,223,372,036,854,775,807', '9,007,199,254,740,993', '75.00%', '25.13%', '10.33 ms', '200.50 ms'])
  })
  it('shows missing values as dashes and preserves real zero', () => {
    render(<OverallMetrics entry={{ ...entry, details: { ...entry.details!, cache_read_rate: null, ttft_mean_ms: null, latency_mean_ms: '0.00' } }} />)
    expect(screen.getAllByText('—')).toHaveLength(2)
    expect(screen.getByText('0.00')).toHaveTextContent('0.00 ms')
  })
  it('does not fabricate measurements for older responses', () => {
    render(<OverallMetrics entry={{ ...entry, details: undefined }} />)
    expect(screen.getAllByText('—')).toHaveLength(6)
  })
})
