import { AdminEntry } from '../admin/AdminEntry'
import { Link } from '../Link'

export function SiteHeader({ admin = false }: { admin?: boolean }) {
  const brand = <><img className="project-icon" src="/assets/brand/logo.svg" width="30" height="30" alt="" /><span>CPA Usage Ranking</span></>
  return <header className="header-wrap"><div className="site-header page-width">
    {admin ? <Link className="brand" href="/" aria-label="CPA Usage Ranking 首页">{brand}</Link> : <a className="brand" href="#overview" aria-label="CPA Usage Ranking 首页">{brand}</a>}
    <nav aria-label="页面导航">{admin ? <Link href="/">排行榜</Link> : <a className="nav-current" href="#ranking" aria-current="page">排行榜</a>}<AdminEntry current={admin} /></nav>
  </div></header>
}
