import App from './App'
import AdminApp from './admin/AdminApp'
import { SessionProvider } from './admin/SessionProvider'
import { usePathname } from './navigation'

export default function RootApp() {
  const admin = usePathname().startsWith('/admin/')
  return <SessionProvider>
    <div hidden={admin}><App active={!admin} /></div>
    {admin && <AdminApp />}
  </SessionProvider>
}
