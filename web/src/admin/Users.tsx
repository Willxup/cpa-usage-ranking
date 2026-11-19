import { useEffect, useState, type FormEvent } from 'react'
import { fetchAvatarCatalog, type AvatarCatalog } from '../api/leaderboards'
import { Avatar } from '../components/LeaderboardTable'
import { AdminError, adminRequest, jsonRequest, type AdminSession } from './api'
import { Link } from '../Link'
import { ConfirmDialog } from './ConfirmDialog'

type User = { id: string; display_name: string; avatar_id: string; created_at: string; disabled: boolean; ranking_hidden: boolean }
type UserPage = { users: User[]; next_cursor: string }

export function Users({ session, onExpired, hiddenOnly = false, active = true }: { session: AdminSession; onExpired: () => void; hiddenOnly?: boolean; active?: boolean }) {
  const searchId = hiddenOnly ? 'hidden-users-search' : 'user-search-query'
  const [query, setQuery] = useState('limit=50')
  const [cursors, setCursors] = useState<string[]>([''])
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{ key: string; data?: UserPage; error?: string; page: number }>({ key: '', page: 1 })
  const [avatars, setAvatars] = useState<AvatarCatalog | null>(null)
  const [busy, setBusy] = useState('')
  const [action, setAction] = useState<{ kind: 'ban' | 'restore'; user: User } | null>(null)
  const cursor = cursors[cursors.length - 1]
  const path = `users?${query}${hiddenOnly ? '&hidden=true' : ''}${cursor ? `&after=${cursor}` : ''}`
  const key = `${path}/${attempt}`
  const loading = result.key !== key
  const data = result.data
  const stale = loading || !!result.error
  useEffect(() => {
    if (!active) return
    return () => { setAction(null); setAttempt((old) => old + 1) }
  }, [active])
  useEffect(() => {
    if (!active) return
    let current = true
    adminRequest<UserPage>(path).then((value) => { if (current) setResult({ key, data: value, page: cursors.length }) }).catch((error: Error) => {
      if (current) { setResult((previous) => ({ ...previous, key, error: error.message })); if (error instanceof AdminError && error.status === 401) onExpired() }
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
  return <section aria-labelledby={hiddenOnly ? undefined : 'users-title'} aria-label={hiddenOnly ? '已下榜用户' : undefined}>
    {!hiddenOnly && <div className="admin-heading"><div><p className="eyebrow">REGISTERED USERS</p><h1 id="users-title">用户管理</h1></div></div>}
    <form className="admin-search" onSubmit={search}>
      <label className="sr-only" htmlFor={searchId}>{active ? '昵称或ID' : ''}</label><input id={searchId} name="query" type="search" placeholder="搜索昵称或 ID" maxLength={80} autoComplete="off" />
      <button className="admin-button" type="submit" disabled={loading || !!busy}>查询</button>
    </form>
    {!data && loading && <div className="admin-list-placeholder" aria-hidden="true" />}
    {!loading && result.error && <div className="admin-list-error"><p className="admin-error" role="alert">{result.error}</p><button className="admin-button" onClick={() => setAttempt((old) => old + 1)}>重试用户列表</button></div>}
    {data && <>
      {data.users.length === 0 ? <p className="admin-empty">未找到匹配的用户。</p> : <table className="admin-table admin-users" aria-busy={loading}><caption className="sr-only">已注册用户</caption><colgroup><col className="admin-col-id" /><col className="admin-col-user" /><col className="admin-col-time" /><col className="admin-col-status" /><col className="admin-col-status" /><col className="admin-col-action" /></colgroup><thead><tr><th scope="col">ID</th><th scope="col">用户</th><th scope="col">注册时间（UTC）</th><th scope="col">封禁状态</th><th scope="col">下榜状态</th><th scope="col">操作</th></tr></thead><tbody>{data.users.map((user) => <tr key={user.id}>
        <td data-label="ID">{user.id}</td><td data-label="用户"><div className="participant"><Avatar id={user.avatar_id} catalog={avatars} /><span>{user.display_name}</span></div></td><td data-label="注册时间（UTC）">{user.created_at.slice(0, 19).replace('T', ' ')}</td><td data-label="封禁状态">{user.disabled ? '已封禁' : '正常'}</td><td data-label="下榜状态">{user.ranking_hidden ? '已下榜' : '未下榜'}</td><td data-label="操作">{hiddenOnly ? <div className="admin-restore-actions"><button className="admin-button admin-button--moderation" disabled={stale || !!busy || user.disabled} onClick={() => setAction({ kind: 'restore', user })}>{busy === user.id ? '正在保存…' : '上榜'}</button>{user.disabled && <><p className="admin-note">先解封，再恢复</p><Link href="/admin/users">前往用户管理</Link></>}</div> : <button className={`admin-button admin-button--moderation${user.disabled ? '' : ' admin-button--danger'}`} disabled={stale || !!busy} onClick={() => setAction({ kind: 'ban', user })}>{busy === user.id ? '正在保存…' : user.disabled ? '解封' : '封禁'}</button>}</td>
      </tr>)}</tbody></table>}
      <div className="admin-pagination"><span>第 {result.page} 页 · {data.users.length} 位用户</span><button className="admin-button" disabled={stale || !!busy || cursors.length === 1} onClick={() => setCursors((old) => old.slice(0, -1))}>上一页</button><button className="admin-button" disabled={stale || !!busy || !data.next_cursor} onClick={() => setCursors((old) => [...old, data.next_cursor])}>下一页</button></div>
    </>}
    {action && <ConfirmDialog title={`${action.kind === 'restore' ? '上榜' : action.user.disabled ? '解封' : '封禁'}“${action.user.display_name}”？`} detail={action.kind === 'ban' && !action.user.disabled ? '停止上报并退出全部榜单，保留统计。' : undefined} onCancel={() => setAction(null)} onConfirm={() => action.kind === 'restore' ? restore(action.user) : setBan(action.user)} />}
  </section>
}
