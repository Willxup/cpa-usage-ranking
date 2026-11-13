import { lazy, Suspense } from 'react'
import App from './App'

const AdminApp = lazy(() => import('./admin/AdminApp'))

export default function RootApp() {
  return <Suspense fallback={<p role="status">正在加载…</p>}>
    {window.location.pathname.startsWith('/admin/') ? <AdminApp /> : <App />}
  </Suspense>
}
