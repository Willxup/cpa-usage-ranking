import type { Board, Period } from './api/leaderboards'

export const boards: Record<Board, { label: string; metric: string; unit: string }> = {
  overall: { label: '综合总榜', metric: '总分', unit: '分' },
  usage: { label: '用量榜', metric: 'Token 用量', unit: 'tokens' },
  cache: { label: '缓存复用榜', metric: '缓存复用率', unit: '%' },
  success: { label: '成功率榜', metric: '成功率得分', unit: '分' },
  ttft: { label: '首字响应榜', metric: '平均首字', unit: 'ms' },
}
export const periods: Record<Period, string> = { today: '今日', yesterday: '昨日', week: '本周', month: '本月' }
