import type { Board, Period } from './api/leaderboards'

export const BOARD_ORDER: readonly Board[] = ['overall', 'usage', 'cache', 'success', 'ttft']
export const PERIOD_ORDER: readonly Period[] = ['today', 'yesterday', 'week', 'month']
