import { useEffect, useState, type FormEvent } from 'react'
import { fetchAvatarCatalog, type AvatarCatalog } from '../api/leaderboards'
import { Avatar } from '../components/LeaderboardTable'
import { AdminError, adminRequest, jsonRequest, type AdminSession } from './api'
import { Link } from '../Link'

type User = { id: string; display_name: string; avatar_id: string; created_at: string; disabled: boolean; ranking_hidden: boolean }
type UserPage = { users: User[]; next_cursor: string }

export function Users({ session, onExpired, hiddenOnly = false }: { session: AdminSession; onExpired: () => void; hiddenOnly?: boolean }) {
  const Heading = hiddenOnly ? 'h2' : 'h1'
  const [query, setQuery] = useState('limit=50')
  const [cursors, setCursors] = useState<string[]>([''])
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{ key: string; data?: UserPage; error?: string }>({ key: '' })
  const [avatars, setAvatars] = useState<AvatarCatalog | null>(null)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  const [actionError, setActionError] = useState('')
  const cursor = cursors[cursors.length - 1]
  const path = `users?${query}${hiddenOnly ? '&hidden=true' : ''}${cursor ? `&after=${cursor}` : ''}`
  const key = `${path}/${attempt}`
  const loading = result.key !== key
  const data = result.data
  useEffect(() => {
    let active = true
    adminRequest<UserPage>(path).then((value) => { if (active) setResult({ key, data: value }) }).catch((error: Error) => {
      if (active) { setResult({ key, error: error.message }); if (error instanceof AdminError && error.status === 401) onExpired() }
    })
    return () => { active = false }
  }, [path, key, onExpired])
  useEffect(() => {
    const controller = new AbortController()
    fetchAvatarCatalog(controller.signal).then(setAvatars).catch(() => { /* Reuse the neutral fallback. */ })
    return () => controller.abort()
  }, [])
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
    if (!user.disabled && !window.confirm(`封禁“${user.display_name}”（ID ${user.id}）？停止上报并退出全部榜单，保留统计。`)) return
    setBusy(user.id); setNotice(''); setActionError('')
    try {
      await adminRequest(`users/${user.id}/ban`, jsonRequest('PUT', { disabled: !user.disabled }, session.csrf_token))
      setNotice(`${user.disabled ? user.ranking_hidden ? '已解封，但仍处于下榜状态，需到榜单管理的“已下榜用户”手动恢复。' : '已解封，按原资格规则恢复参榜。' : '已封禁。'}设置已保存，榜单刷新后生效。`)
      setAttempt((old) => old + 1)
    } catch (error) { setActionError((error as Error).message); if (error instanceof AdminError && error.status === 401) onExpired() }
    finally { setBusy('') }
  }
  async function restore(user: User) {
    if (busy || user.disabled) return
    setBusy(user.id); setNotice(''); setActionError('')
    try {
      await adminRequest(`users/${user.id}/ranking-visibility`, jsonRequest('PUT', { hidden: false }, session.csrf_token))
      setNotice('参榜资格已恢复，不保证进入前 100 或恢复原名次。设置已保存，榜单刷新后生效。')
      setAttempt((old) => old + 1)
    } catch (error) { setActionError((error as Error).message); if (error instanceof AdminError && error.status === 401) onExpired() }
    finally { setBusy('') }
  }
  return <section aria-labelledby="users-title">
    <div className="admin-heading"><div><p className="eyebrow">{hiddenOnly ? 'EXCLUDED USERS' : 'REGISTERED USERS'}</p><Heading id="users-title">{hiddenOnly ? '已下榜用户' : '用户管理'}</Heading></div></div>
    <form className="admin-search" onSubmit={search}>
      <label className="sr-only" htmlFor="user-search-query">昵称或ID</label><input id="user-search-query" name="query" type="search" placeholder="搜索昵称或 ID" maxLength={80} autoComplete="off" />
      <button className="admin-button" type="submit" disabled={loading || !!busy}>查询</button>
    </form>
    {notice && <p className="admin-notice" role="status">{notice}</p>}
    {actionError && <p className="admin-error" role="alert">{actionError}</p>}
    {loading && <div className="admin-progress" role="progressbar" aria-label="读取用户列表" />}
    {!loading && result.error && <div className="admin-list-error"><p className="admin-error" role="alert">{result.error}</p><button className="admin-button" onClick={() => setAttempt((old) => old + 1)}>重试用户列表</button></div>}
    {data && <>
      {data.users.length === 0 ? <p className="admin-empty">未找到匹配的用户。</p> : <table className="admin-table admin-users" aria-busy={loading}><caption className="sr-only">已注册用户</caption><thead><tr><th scope="col">ID</th><th scope="col">用户</th><th scope="col">注册时间（UTC）</th><th scope="col">封禁状态</th><th scope="col">下榜状态</th><th scope="col">操作</th></tr></thead><tbody>{data.users.map((user) => <tr key={user.id}>
        <td data-label="ID">{user.id}</td><td data-label="用户"><div className="participant"><Avatar id={user.avatar_id} catalog={avatars} /><span>{user.display_name}</span></div></td><td data-label="注册时间（UTC）">{user.created_at.slice(0, 19).replace('T', ' ')}</td><td data-label="封禁状态">{user.disabled ? '已封禁' : '正常'}</td><td data-label="下榜状态">{user.ranking_hidden ? '已下榜' : '未下榜'}</td><td data-label="操作">{hiddenOnly ? <div className="admin-restore-actions"><button className="admin-button admin-button--moderation" disabled={loading || !!busy || user.disabled} onClick={() => void restore(user)}>{busy === user.id ? '正在保存…' : '恢复上榜'}</button>{user.disabled && <><p className="admin-note">先解封，再恢复</p><Link href="/admin/users">前往用户管理</Link></>}</div> : <button className={`admin-button admin-button--moderation${user.disabled ? '' : ' admin-button--danger'}`} disabled={loading || !!busy} onClick={() => void setBan(user)}>{busy === user.id ? '正在保存…' : user.disabled ? '解封' : '封禁'}</button>}</td>
      </tr>)}</tbody></table>}
      <div className="admin-pagination"><span>第 {cursors.length} 页 · {data.users.length} 位用户</span><button className="admin-button" disabled={loading || !!busy || cursors.length === 1} onClick={() => setCursors((old) => old.slice(0, -1))}>上一页</button><button className="admin-button" disabled={loading || !!busy || !data.next_cursor} onClick={() => setCursors((old) => [...old, data.next_cursor])}>下一页</button></div>
    </>}
  </section>
}
