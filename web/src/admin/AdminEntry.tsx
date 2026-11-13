import { useSession } from './useSession'

export function AdminEntry() {
  const { session } = useSession()
  if (!session?.enabled) return null
  return <a className="admin-entry" href={session.authenticated ? '/admin/leaderboards' : '/admin/login'}>{session.authenticated ? '控制台' : '登录'}</a>
}
