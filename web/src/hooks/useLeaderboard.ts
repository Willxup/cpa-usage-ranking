import { useEffect, useState } from 'react'
import { fetchAvatarCatalog, fetchLeaderboard } from '../api/leaderboards'
import type { AvatarCatalog, Board, Leaderboard, Period } from '../api/leaderboards'
import { formatLocalizedError, useI18n } from '../i18n'

type Result = { key: string; data: Leaderboard | null; failed: boolean; error: unknown }

export function useLeaderboard(board: Board, period: Period, selectionVersion: number, enabled = true) {
  const { locale } = useI18n()
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<Result>({ key: '', data: null, failed: false, error: null })
  const [avatars, setAvatars] = useState<AvatarCatalog | null>(null)
  const key = `${board}/${period}/${selectionVersion}/${attempt}`
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    let active = true
    // 选择变化、重试和卸载都取消旧请求；即使服务忽略取消也不接纳旧响应。
    fetchLeaderboard(board, period, controller.signal).then((data) => {
      if (active) setResult({ key, data, failed: false, error: null })
    }).catch((error: unknown) => {
      if (active) setResult((previous) => ({ key, data: previous.data, failed: true, error }))
    })
    return () => { active = false; controller.abort() }
  }, [board, period, key, enabled])
  useEffect(() => {
    if (!enabled || avatars) return
    const controller = new AbortController()
    let active = true
    fetchAvatarCatalog(controller.signal).then((catalog) => { if (active) setAvatars(catalog) }).catch(() => { /* 无目录时显示中性头像，不影响榜单。 */ })
    return () => { active = false; controller.abort() }
  }, [enabled, avatars, key])
  const loading = result.key !== key
  const error = loading || !result.failed ? null : formatLocalizedError(result.error, (m) => m.leaderboardErrors.generic, locale)
  return { data: result.data, loading, error, avatars, retry: () => setAttempt((value) => value + 1) }
}
