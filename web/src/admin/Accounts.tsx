import { useEffect, useState, type FormEvent } from 'react'
import { adminRequest, jsonRequest, type AdminSession } from './api'

type Account = { id: string; username: string; created_at: string }
type AccountsData = { owner: { username: string }; accounts: Account[] }

export function Accounts({ session, onExpired }: { session: AdminSession; onExpired: () => void }) {
  const [data, setData] = useState<AccountsData | null>(null)
  const [error, setError] = useState('')
  const [createError, setCreateError] = useState('')
  const [version, setVersion] = useState(0)
  const [loaded, setLoaded] = useState(-1)
  const loading = loaded !== version
  const [busy, setBusy] = useState('')
  useEffect(() => {
    let active = true
    adminRequest<AccountsData>('accounts').then((value) => {
      if (active) { setData(value); setError(''); setLoaded(version) }
    }).catch((error: Error) => { if (active) { setError(error.message); setLoaded(version); onExpired() } })
    return () => { active = false }
  }, [version, onExpired])
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const element = event.currentTarget
    const form = new FormData(element)
    setBusy('create'); setCreateError('')
    try {
      await adminRequest('accounts', jsonRequest('POST', { username: form.get('username'), password: form.get('password') }, session.csrf_token))
      element.reset(); setVersion((old) => old + 1)
    } catch (error) { setCreateError((error as Error).message); onExpired() }
    finally { setBusy('') }
  }
  async function remove(account: Account) {
    if (busy || !window.confirm(`删除管理员“${account.username}”（ID ${account.id}）？其现有会话将失效。`)) return
    setBusy(account.id); setError('')
    try {
      await adminRequest(`accounts/${account.id}`, jsonRequest('DELETE', {}, session.csrf_token))
      setVersion((old) => old + 1)
    } catch (error) { setError((error as Error).message); onExpired() }
    finally { setBusy('') }
  }
  return <section aria-labelledby="accounts-title">
    <div className="admin-heading"><div><p className="eyebrow">ADMINISTRATORS</p><h1 id="accounts-title">管理员管理</h1></div></div>
    {error && <div><p className="admin-error" role="alert">{error}</p><button className="admin-button" disabled={loading || !!busy} onClick={() => setVersion((old) => old + 1)}>重新加载列表</button></div>}
    <div className="admin-accounts-layout">
      <section className="admin-card admin-account-form" aria-labelledby="add-account-title"><h2 id="add-account-title">添加管理员</h2><form onSubmit={(event) => void create(event)} aria-busy={busy === 'create'}>
        <label htmlFor="account-username">用户名</label><input id="account-username" name="username" autoComplete="off" required minLength={3} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_.\-]{2,63}" />
        <label htmlFor="account-password">初始密码</label><input id="account-password" name="password" type="password" autoComplete="new-password" required minLength={12} maxLength={72} />
        {createError && <p className="admin-error" role="alert">{createError}</p>}
        <button className="admin-button admin-button--primary" type="submit" disabled={!!busy}>{busy === 'create' ? '正在添加…' : '添加管理员'}</button>
      </form></section>
      <section className="admin-card admin-account-list" aria-labelledby="accounts-list-title" aria-busy={loading}>
        <h2 id="accounts-list-title">管理员列表</h2>
        {loading && <div className="admin-progress" role="progressbar" aria-label="读取管理员列表" />}
        {data && (data.accounts.length === 0 ? <p className="admin-empty">暂无管理员。</p> : <table className="admin-table"><caption className="sr-only">管理员列表</caption><thead><tr><th scope="col">ID</th><th scope="col">用户名</th><th scope="col">创建时间（UTC）</th><th scope="col">操作</th></tr></thead><tbody>{data.accounts.map((account) => <tr key={account.id}>
          <td data-label="ID">{account.id}</td><td data-label="用户名">{account.username}</td><td data-label="创建时间（UTC）">{account.created_at.slice(0, 19).replace('T', ' ')}</td><td data-label="操作"><button className="admin-button admin-button--danger admin-button--moderation" disabled={!!busy} onClick={() => void remove(account)}>{busy === account.id ? '正在删除…' : '删除'}</button></td>
        </tr>)}</tbody></table>)}
      </section>
    </div>
  </section>
}
