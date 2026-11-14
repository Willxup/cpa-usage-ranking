import { useEffect, useRef, useState, type FormEvent } from 'react'
import type { Board, Period } from '../api/leaderboards'
import { LeaderboardPanel } from '../components/LeaderboardPanel'
import { boards, periods } from '../leaderboardLabels'
import { adminRequest, jsonRequest } from './api'
import { useSession } from './useSession'
import { Accounts } from './Accounts'
import { Users } from './Users'
import '../styles/app.css'
import './admin.css'

function Login() {
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
      await adminRequest('session', jsonRequest('POST', { username: form.get('username'), password: form.get('password') }))
      window.location.assign('/admin/leaderboards')
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

function CurrentLeaderboards() {
  const [selection, setSelection] = useState<{ board: Board; period: Period; version: number }>({ board: 'overall', period: 'today', version: 0 })
  const { board, period } = selection
  return <section aria-label="榜单管理">
    <div className="admin-heading"><div><p className="eyebrow">LEADERBOARDS</p><h1>榜单查看</h1></div><p className="admin-note">与公开页面使用同一份榜单缓存。</p></div>
    <div className="ranking-toolbar">
      <fieldset className="board-control"><legend className="sr-only">榜单类型</legend>{(Object.keys(boards) as Board[]).map((item) => <button key={item} type="button" aria-pressed={board === item} onClick={() => setSelection((old) => ({ ...old, board: item, version: old.version + 1 }))}>{boards[item].label}</button>)}</fieldset>
      <fieldset className="period-control"><legend className="sr-only">时间范围</legend>{(Object.keys(periods) as Period[]).map((item) => <button key={item} type="button" aria-pressed={period === item} onClick={() => setSelection((old) => ({ ...old, period: item, version: old.version + 1 }))}>{periods[item]}</button>)}</fieldset>
    </div>
    <LeaderboardPanel board={board} period={period} selectionVersion={selection.version} />
  </section>
}

export default function AdminApp() {
  const { session, error, refresh } = useSession()
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState('')
  const login = window.location.pathname === '/admin/login'
  const accounts = window.location.pathname === '/admin/accounts'
  const users = window.location.pathname === '/admin/users'
  const main = useRef<HTMLElement>(null)
  useEffect(() => { main.current?.focus() }, [])
  useEffect(() => {
    if (session?.enabled && !session.authenticated && !login) window.location.replace('/admin/login')
    if (session?.authenticated && login) window.location.replace('/admin/leaderboards')
  }, [session, login])
  async function logout() {
    if (busy) return
    setBusy(true); setActionError('')
    try { await adminRequest('session', jsonRequest('DELETE', {}, session?.csrf_token)); window.location.assign('/') }
    catch (error) { setActionError((error as Error).message); setBusy(false); void refresh() }
  }
  return <div className="app-shell admin-shell">
    <header className="header-wrap"><div className="site-header page-width">
      <a className="brand" href="/" aria-label="CPA Usage Ranking 首页"><img className="project-icon" src="/assets/brand/project-icon.svg" width="40" height="40" alt="" /><span>CPA Usage Ranking</span></a>
      <a className="admin-entry" href="/">公开榜单</a>
    </div></header>
    <main id="main" className="page-width admin-main" tabIndex={-1} ref={main}>
      {!session && !error && <p role="status">正在检查会话…</p>}
      {error && <div role="alert"><p className="admin-error">{error}</p><button className="admin-button" onClick={() => void refresh()}>重试</button></div>}
      {session && !session.enabled && <section className="admin-card"><h1>管理后台未启用</h1><a className="primary-link" href="/">返回公开榜单</a></section>}
      {session?.enabled && !session.authenticated && login && <Login />}
      {session?.authenticated && !login && <>
        <div className="admin-nav-wrap"><nav className="admin-nav" aria-label="控制台导航"><a href="/admin/leaderboards" aria-current={!accounts && !users ? 'page' : undefined}>榜单查看</a><a href="/admin/users" aria-current={users ? 'page' : undefined}>用户管理</a>{session.user?.owner && <a href="/admin/accounts" aria-current={accounts ? 'page' : undefined}>管理员管理</a>}</nav><div className="admin-identity"><span>{session.user?.username} · {session.user?.owner ? '主管理员' : '普通管理员'}</span><button className="admin-button" disabled={busy} onClick={() => void logout()}>{busy ? '正在退出…' : '退出'}</button></div></div>
        {actionError && <p className="admin-error" role="alert">{actionError}</p>}
        {accounts ? session.user?.owner ? <Accounts session={session} onExpired={refresh} /> : <section className="admin-card"><h1>仅主管理员可管理账号</h1><a className="primary-link" href="/admin/leaderboards">返回榜单</a></section> : users ? <Users onExpired={refresh} /> : <CurrentLeaderboards />}
      </>}
    </main>
    <footer className="site-footer page-width"><span>CPA Usage Ranking · 控制台</span><p>统计周期保持 UTC</p></footer>
  </div>
}
