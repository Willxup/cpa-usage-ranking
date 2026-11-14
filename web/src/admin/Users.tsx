import { useEffect, useState, type FormEvent } from 'react'
import { fetchAvatarCatalog, type AvatarCatalog } from '../api/leaderboards'
import { Avatar } from '../components/LeaderboardTable'
import { AdminError, adminRequest } from './api'

type User = { id: string; display_name: string; avatar_id: string; created_at: string; disabled: boolean }
type UserPage = { users: User[]; next_cursor: string }

export function Users({ onExpired }: { onExpired: () => void }) {
  const [query, setQuery] = useState('limit=50')
  const [cursors, setCursors] = useState<string[]>([''])
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{ key: string; data?: UserPage; error?: string }>({ key: '' })
  const [avatars, setAvatars] = useState<AvatarCatalog | null>(null)
  const cursor = cursors[cursors.length - 1]
  const path = `users?${query}${cursor ? `&after=${cursor}` : ''}`
  const key = `${path}/${attempt}`
  const loading = result.key !== key
  const data = loading ? undefined : result.data
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
    const params = new URLSearchParams({ limit: String(form.get('limit')) })
    const term = String(form.get('query')).trim()
    if (term) params.set(String(form.get('kind')), term)
    if (form.get('disabled')) params.set('disabled', String(form.get('disabled')))
    setCursors(['']); setQuery(params.toString()); setAttempt((old) => old + 1)
  }
  return <section aria-labelledby="users-title">
    <div className="admin-heading"><div><p className="eyebrow">REGISTERED USERS</p><h1 id="users-title">用户管理</h1></div><p className="admin-note">所有已注册客户端，包括无上报和已封禁身份。当前列表只读。</p></div>
    <form className="admin-filters" onSubmit={search}>
      <div className="admin-field"><label htmlFor="user-search-kind">查询方式</label><select id="user-search-kind" name="kind" defaultValue="name"><option value="name">昵称包含</option><option value="id">ID 精确查询</option></select></div>
      <div className="admin-field"><label htmlFor="user-search-query">查询内容</label><input id="user-search-query" name="query" type="search" maxLength={80} autoComplete="off" /></div>
      <div className="admin-field"><label htmlFor="user-search-disabled">封禁状态</label><select id="user-search-disabled" name="disabled" defaultValue=""><option value="">全部状态</option><option value="false">正常</option><option value="true">已封禁</option></select></div>
      <div className="admin-field"><label htmlFor="user-search-limit">每页条数</label><select id="user-search-limit" name="limit" defaultValue="50"><option value="50">50</option><option value="100">100</option></select></div>
      <button className="admin-button" type="submit" disabled={loading}>查询</button>
    </form>
    {loading && <p className="admin-empty" role="status">正在加载用户…</p>}
    {!loading && result.error && <div className="admin-list-error"><p className="admin-error" role="alert">{result.error}</p><button className="admin-button" onClick={() => setAttempt((old) => old + 1)}>重试用户列表</button></div>}
    {data && <>
      {data.users.length === 0 ? <p className="admin-empty">未找到匹配的用户。</p> : <table className="admin-table admin-users"><caption className="sr-only">已注册用户</caption><thead><tr><th scope="col">ID</th><th scope="col">用户</th><th scope="col">注册时间（UTC）</th><th scope="col">封禁状态</th></tr></thead><tbody>{data.users.map((user) => <tr key={user.id}>
        <td data-label="ID">{user.id}</td><td data-label="用户"><div className="participant"><Avatar id={user.avatar_id} catalog={avatars} /><span>{user.display_name}</span></div></td><td data-label="注册时间（UTC）">{user.created_at.slice(0, 19).replace('T', ' ')}</td><td data-label="封禁状态">{user.disabled ? '已封禁' : '正常'}</td>
      </tr>)}</tbody></table>}
      <div className="admin-pagination"><span>第 {cursors.length} 页 · {data.users.length} 位用户</span><button className="admin-button" disabled={cursors.length === 1} onClick={() => setCursors((old) => old.slice(0, -1))}>上一页</button><button className="admin-button" disabled={!data.next_cursor} onClick={() => setCursors((old) => [...old, data.next_cursor])}>下一页</button></div>
    </>}
  </section>
}
