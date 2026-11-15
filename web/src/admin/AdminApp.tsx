import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Leaderboards } from './Leaderboards'
import { adminRequest, jsonRequest, type AdminSession } from './api'
import { Link } from '../Link'
import { navigate, usePathname } from '../navigation'
import { useSession } from './useSession'
import { Accounts } from './Accounts'
import { Users } from './Users'
import '../styles/app.css'
import './admin.css'

function Login() {
  const { accept } = useSession()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const errorRef = useRef<HTMLParagraphElement>(null)
  const [showPassword, setShowPassword] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const form = new FormData(event.currentTarget)
    setBusy(true); setError('')
    try {
      const value = await adminRequest<AdminSession>('session', jsonRequest('POST', { username: form.get('username'), password: form.get('password') }))
      accept(value)
      navigate('/admin/leaderboards', true)
    } catch (error) { setError((error as Error).message); setBusy(false) }
  }
  useEffect(() => { if (error) errorRef.current?.focus() }, [error])
  return <section className="admin-card admin-login" aria-labelledby="login-title">
    <p className="eyebrow">ADMIN CONSOLE</p><h1 id="login-title">管理员登录</h1>
    <p className="admin-note">使用管理员账号登录。客户端密钥不能用于此处。</p>
    <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
      <label htmlFor="username">用户名</label><input id="username" name="username" autoComplete="username" required maxLength={64} />
      <label htmlFor="password">密码</label><input id="password" name="password" type={showPassword ? 'text' : 'password'} autoComplete="current-password" required maxLength={72} />
      <label className="admin-check"><input type="checkbox" checked={showPassword} onChange={(event) => setShowPassword(event.target.checked)} />显示密码</label>
      {error && <p className="admin-error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}
      <button className="admin-button admin-button--primary" type="submit" disabled={busy}>{busy ? '正在登录…' : '登录'}</button>
    </form>
  </section>
}

export default function AdminApp() {
  const { session, error, refresh, accept } = useSession()
  const path = usePathname()
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState('')
  const login = path === '/admin/login'
  const accounts = path === '/admin/accounts'
  const users = path === '/admin/users'
  const main = useRef<HTMLElement>(null)
  useEffect(() => { main.current?.focus() }, [path])
  useEffect(() => {
    if (session?.enabled && !session.authenticated && !login) navigate('/admin/login', true)
    if (session?.authenticated && login) navigate('/admin/leaderboards', true)
  }, [session, login])
  async function logout() {
    if (busy) return
    setBusy(true); setActionError('')
    try { await adminRequest('session', jsonRequest('DELETE', {}, session?.csrf_token)); accept({ enabled: true, authenticated: false }); navigate('/') }
    catch (error) { setActionError((error as Error).message); setBusy(false); void refresh() }
  }
  return <div className="app-shell admin-shell">
    <header className="header-wrap"><div className="site-header page-width">
      <Link className="brand" href="/" aria-label="CPA Usage Ranking 首页"><img className="project-icon" src="/assets/brand/project-icon.svg" width="40" height="40" alt="" /><span>CPA Usage Ranking</span></Link>
      <Link className="admin-entry" href="/">公开榜单</Link>
    </div></header>
    <main id="main" className="page-width admin-main" tabIndex={-1} ref={main}>
      {!session && !error && <div className="admin-progress" role="progressbar" aria-label="验证会话" />}
      {error && <div role="alert"><p className="admin-error">{error}</p><button className="admin-button" onClick={() => void refresh()}>重试</button></div>}
      {session && !session.enabled && <section className="admin-card"><h1>管理后台未启用</h1><Link className="primary-link" href="/">返回公开榜单</Link></section>}
      {session?.enabled && !session.authenticated && login && <Login />}
      {session?.authenticated && !login && <>
        <div className="admin-nav-wrap"><nav className="admin-nav" aria-label="控制台导航"><Link href="/admin/leaderboards" aria-current={!accounts && !users ? 'page' : undefined}>榜单管理</Link><Link href="/admin/users" aria-current={users ? 'page' : undefined}>用户管理</Link>{session.user?.owner && <Link href="/admin/accounts" aria-current={accounts ? 'page' : undefined}>管理员管理</Link>}</nav><div className="admin-identity"><span>{session.user?.username}</span><button className="admin-button" disabled={busy} onClick={() => void logout()}>{busy ? '正在退出…' : '退出'}</button></div></div>
        {actionError && <p className="admin-error" role="alert">{actionError}</p>}
        {accounts ? session.user?.owner ? <Accounts session={session} onExpired={refresh} /> : <section className="admin-card"><h1>无访问权限</h1><Link className="primary-link" href="/admin/leaderboards">返回榜单</Link></section> : users ? <Users session={session} onExpired={refresh} /> : <Leaderboards session={session} onExpired={refresh} />}
      </>}
    </main>
    <footer className="site-footer page-width"><span>CPA Usage Ranking · 控制台</span><p>统计周期保持 UTC</p></footer>
  </div>
}
