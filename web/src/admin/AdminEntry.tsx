import { useI18n } from '../i18n'
import { Link } from '../Link'
import { useSession } from './useSession'

export function AdminEntry({ current = false }: { current?: boolean }) {
  const { m } = useI18n()
  const { session } = useSession()
  if (!session?.enabled) return null
  return <Link className={current ? 'nav-current' : 'admin-entry'} aria-current={current ? 'page' : undefined} href={session.authenticated ? '/admin/leaderboards' : '/admin/login'}>{session.authenticated ? m.nav.console : m.nav.login}</Link>
}
