import { ArrowIcon } from './Brand'
import { StarButton } from './ui/StarButton'

export type ConnectionState = 'idle' | 'loading' | 'online' | 'error'

type Props = { state: ConnectionState; onCheck?: () => void; message?: string }

const labels: Record<ConnectionState, { title: string; detail: string }> = {
  idle: { title: '尚未检测', detail: '检查服务连接，只验证进程是否存活。' },
  loading: { title: '正在连接', detail: '正在检查服务进程，请稍候。' },
  online: { title: '服务在线', detail: '存活检查通过，榜单状态以实际加载结果为准。' },
  error: { title: '连接失败', detail: '暂时无法连接服务，可以稍后重试。' },
}

export function StatusPanel({ state, onCheck, message }: Props) {
  const status = labels[state]
  return (
    <section id="status" className="status-panel glass-panel" aria-labelledby="status-heading">
      <div className="section-topline"><span className="eyebrow">LIVE CONNECTION</span><span className="endpoint-label">GET /healthz</span></div>
      <div className="status-heading-row">
        <h2 id="status-heading">保持连接。</h2>
        <span className={`signal-icon signal-icon--${state}`} aria-hidden="true"><span /><span /><span /></span>
      </div>
      <div className={`connection-status connection-status--${state}`} role="status" aria-live="polite" aria-atomic="true">
        <span className="connection-dot" aria-hidden="true" />
        <div><strong>{status.title}</strong><p>{message ?? status.detail}</p></div>
      </div>
      <dl className="service-facts">
        <div><dt>运行状态</dt><dd>{state === 'online' ? '进程存活' : state === 'loading' ? '正在检测' : state === 'error' ? '未能确认' : '尚未检测'}</dd></div>
        <div><dt>榜单状态</dt><dd>以实际加载为准</dd></div>
      </dl>
      <StarButton onClick={onCheck} disabled={!onCheck || state === 'loading'} aria-busy={state === 'loading'}>
        {state === 'loading' ? '正在检查…' : '检查服务连接'}<ArrowIcon />
      </StarButton>
      <p className="status-footnote">连接成功不代表榜单可用。我们不会用演示数据冒充真实用量。</p>
    </section>
  )
}
