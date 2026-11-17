import { Link } from '../Link'
import { useSession } from './useSession'

export function AdminEntry({ current = false }: { current?: boolean }) {
  const { session } = useSession()
  if (!session?.enabled) return null
  return <Link className={current ? 'nav-current' : 'admin-entry'} aria-current={current ? 'page' : undefined} href={session.authenticated ? '/admin/leaderboards' : '/admin/login'}>{session.authenticated ? '控制台' : '登录'}</Link>
}
