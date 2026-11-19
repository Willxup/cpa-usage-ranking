import { useEffect, useState, type FormEvent } from 'react'
import { fetchAvatarCatalog, type AvatarCatalog } from '../api/leaderboards'
import { Avatar } from '../components/LeaderboardTable'
import { formatLocalizedError, useI18n } from '../i18n'
import { AdminError, adminRequest, jsonRequest, type AdminSession } from './api'
import { Link } from '../Link'
import { ConfirmDialog } from './ConfirmDialog'

type User = { id: string; display_name: string; avatar_id: string; created_at: string; disabled: boolean; ranking_hidden: boolean }
type UserPage = { users: User[]; next_cursor: string }

export function Users({ session, onExpired, hiddenOnly = false, active = true }: { session: AdminSession; onExpired: () => void; hiddenOnly?: boolean; active?: boolean }) {
  const { locale, m } = useI18n()
  const searchId = hiddenOnly ? 'hidden-users-search' : 'user-search-query'
  const [query, setQuery] = useState('limit=50')
  const [cursors, setCursors] = useState<string[]>([''])
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{ key: string; data?: UserPage; failed?: boolean; error?: unknown; page: number }>({ key: '', page: 1 })
  const [avatars, setAvatars] = useState<AvatarCatalog | null>(null)
  const [busy, setBusy] = useState('')
  const [action, setAction] = useState<{ kind: 'ban' | 'restore'; user: User } | null>(null)
  const cursor = cursors[cursors.length - 1]
  const path = `users?${query}${hiddenOnly ? '&hidden=true' : ''}${cursor ? `&after=${cursor}` : ''}`
  const key = `${path}/${attempt}`
  const loading = result.key !== key
  const data = result.data
  const stale = loading || !!result.failed
  const errorMessage = !loading && result.failed
    ? formatLocalizedError(result.error, (msg) => msg.admin.errors.network, locale)
    : ''
  useEffect(() => {
    if (!active) return
    return () => { setAction(null); setAttempt((old) => old + 1) }
  }, [active])
  useEffect(() => {
    if (!active) return
    let current = true
    adminRequest<UserPage>(path).then((value) => { if (current) setResult({ key, data: value, failed: false, error: undefined, page: cursors.length }) }).catch((err: unknown) => {
      if (current) { setResult((previous) => ({ ...previous, key, failed: true, error: err })); if (err instanceof AdminError && err.status === 401) onExpired() }
    })
    return () => { current = false }
  }, [path, key, onExpired, active, cursors.length])
  useEffect(() => {
    if (!active || avatars) return
    const controller = new AbortController()
    fetchAvatarCatalog(controller.signal).then(setAvatars).catch(() => { /* Reuse the neutral fallback. */ })
    return () => controller.abort()
  }, [active, avatars, key])
  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const params = new URLSearchParams({ limit: '50' })
    const term = String(form.get('query')).trim()
    if (term) params.set('q', term)
    setCursors(['']); setQuery(params.toString()); setAttempt((old) => old + 1)
  }
  async function setBan(user: User) {
    if (busy) return
    setBusy(user.id)
    try {
      await adminRequest(`users/${user.id}/ban`, jsonRequest('PUT', { disabled: !user.disabled }, session.csrf_token))
      setAttempt((old) => old + 1)
    } catch (error) { if (error instanceof AdminError && error.status === 401) onExpired(); throw error }
    finally { setBusy('') }
  }
  async function restore(user: User) {
    if (busy || user.disabled) return
    setBusy(user.id)
    try {
      await adminRequest(`users/${user.id}/ranking-visibility`, jsonRequest('PUT', { hidden: false }, session.csrf_token))
      setAttempt((old) => old + 1)
    } catch (error) { if (error instanceof AdminError && error.status === 401) onExpired(); throw error }
    finally { setBusy('') }
  }
  return <section aria-labelledby={hiddenOnly ? undefined : 'users-title'} aria-label={hiddenOnly ? m.admin.excludedRegionAria : undefined}>
    {!hiddenOnly && <div className="admin-heading"><div><p className="eyebrow">{m.admin.usersEyebrow}</p><h1 id="users-title">{m.admin.usersTitle}</h1></div></div>}
    <form className="admin-search" onSubmit={search}>
      <label className="sr-only" htmlFor={searchId}>{active ? m.admin.searchLabel : ''}</label><input id={searchId} name="query" type="search" placeholder={m.admin.searchPlaceholder} maxLength={80} autoComplete="off" />
      <button className="admin-button" type="submit" disabled={loading || !!busy}>{m.admin.searchButton}</button>
    </form>
    {!data && loading && <div className="admin-list-placeholder" aria-hidden="true" />}
    {errorMessage && <div className="admin-list-error"><p className="admin-error" role="alert">{errorMessage}</p><button className="admin-button" onClick={() => setAttempt((old) => old + 1)}>{m.admin.retryUsersButton}</button></div>}
    {data && <div className="admin-surface">
      {data.users.length === 0 ? <p className="admin-empty">{m.admin.emptyUsers}</p> : <table className="admin-table admin-users" aria-busy={loading}><caption className="sr-only">{m.admin.usersCaption}</caption><colgroup><col className="admin-col-id" /><col className="admin-col-user" /><col className="admin-col-time" /><col className="admin-col-status" /><col className="admin-col-status" /><col className="admin-col-action" /></colgroup><thead><tr><th scope="col">{m.admin.colId}</th><th scope="col">{m.admin.colUser}</th><th scope="col">{m.admin.colRegisteredAt}</th><th scope="col">{m.admin.colBanStatus}</th><th scope="col">{m.admin.colBoardStatus}</th><th scope="col">{m.admin.colAction}</th></tr></thead><tbody>{data.users.map((user) => <tr key={user.id}>
        <td data-label={m.admin.colId}>{user.id}</td><td data-label={m.admin.colUser}><div className="participant"><Avatar id={user.avatar_id} catalog={avatars} /><span>{user.display_name}</span></div></td><td data-label={m.admin.colRegisteredAt}>{user.created_at.slice(0, 19).replace('T', ' ')}</td><td data-label={m.admin.colBanStatus}><span className={`status-pill ${user.disabled ? 'status-pill--danger' : 'status-pill--ok'}`}>{user.disabled ? m.admin.statusBanned : m.admin.statusActive}</span></td><td data-label={m.admin.colBoardStatus}><span className={`status-pill ${user.ranking_hidden ? 'status-pill--warn' : 'status-pill--neutral'}`}>{user.ranking_hidden ? m.admin.statusExcluded : m.admin.statusListed}</span></td><td data-label={m.admin.colAction}>{hiddenOnly ? <div className="admin-restore-actions"><button className="admin-button admin-button--moderation" disabled={stale || !!busy || user.disabled} onClick={() => setAction({ kind: 'restore', user })}>{busy === user.id ? m.admin.savingBusy : m.admin.restoreButton}</button>{user.disabled && <><p className="admin-note">{m.admin.unbanFirstNote}</p><Link href="/admin/users">{m.admin.goToUsersLink}</Link></>}</div> : <button className={`admin-button admin-button--moderation${user.disabled ? '' : ' admin-button--danger'}`} disabled={stale || !!busy} onClick={() => setAction({ kind: 'ban', user })}>{busy === user.id ? m.admin.savingBusy : user.disabled ? m.admin.unbanButton : m.admin.banButton}</button>}</td>
      </tr>)}</tbody></table>}
      <div className="admin-pagination"><span>{m.admin.paginationSummary(result.page, data.users.length)}</span><button className="admin-button" disabled={stale || !!busy || cursors.length === 1} onClick={() => setCursors((old) => old.slice(0, -1))}>{m.admin.prevPage}</button><button className="admin-button" disabled={stale || !!busy || !data.next_cursor} onClick={() => setCursors((old) => [...old, data.next_cursor])}>{m.admin.nextPage}</button></div>
    </div>}
    {action && <ConfirmDialog title={action.kind === 'restore' ? m.admin.confirmRestoreTitle(action.user.display_name) : action.user.disabled ? m.admin.confirmUnbanTitle(action.user.display_name) : m.admin.confirmBanTitle(action.user.display_name)} detail={action.kind === 'ban' && !action.user.disabled ? m.admin.confirmBanDetail : undefined} onCancel={() => setAction(null)} onConfirm={() => action.kind === 'restore' ? restore(action.user) : setBan(action.user)} />}
  </section>
}
