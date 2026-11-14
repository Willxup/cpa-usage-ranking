import { fetchLeaderboard, type Board, type Period } from '../api/leaderboards'

// A bounded foreground wait after this administrator's mutation, not a second
// refresh worker or recurring polling cycle. Rankings always come from the
// existing public API; no client-side row removal or score reconstruction.
export async function waitForExclusion(board: Board, period: Period, previousAt: string, id: string) {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 8_000)
  try {
    while (!controller.signal.aborted) {
      const data = await fetchLeaderboard(board, period, controller.signal)
      if (data.generated_at !== previousAt && !data.entries.some((entry) => entry.client_id === id)) return true
      await new Promise((resolve) => window.setTimeout(resolve, 250))
    }
  } catch { /* The saved state remains valid; the UI offers an explicit retry. */ }
  finally { window.clearTimeout(timeout) }
  return false
}
