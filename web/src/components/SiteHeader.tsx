import { AdminEntry } from '../admin/AdminEntry'
import { useI18n } from '../i18n'
import { Link } from '../Link'
import { LanguageSwitcher } from './LanguageSwitcher'

export function SiteHeader({ admin = false }: { admin?: boolean }) {
  const { m } = useI18n()
  const brand = <><img className="project-icon" src="/assets/brand/logo.svg" width="30" height="30" alt="" /><span>CPA Usage Ranking</span></>
  return <header className="header-wrap"><div className="site-header page-width">
    {admin ? <Link className="brand" href="/" aria-label={m.nav.homeAria}>{brand}</Link> : <a className="brand" href="#overview" aria-label={m.nav.homeAria}>{brand}</a>}
    <div className="header-actions">
      <LanguageSwitcher />
      <nav aria-label={m.nav.pageNav}>{admin ? <Link href="/">{m.nav.leaderboards}</Link> : <a className="nav-current" href="#ranking" aria-current="page">{m.nav.leaderboards}</a>}<AdminEntry current={admin} /></nav>
    </div>
  </div></header>
}
