import { ArrowIcon, BrandMark } from './components/Brand'
import { StatusPanel } from './components/StatusPanel'
import { useHealth } from './hooks/useHealth'
import './styles/app.css'

export default function App() {
  const health = useHealth()
  return (
    <div className="app-shell">
      <header className="header-wrap">
        <div className="site-header page-width">
          <a className="brand" href="#overview" aria-label="CPA Usage Ranking 首页"><img className="project-icon" src="/assets/brand/project-icon.svg" width="40" height="40" alt="" /><span>CPA Usage Ranking</span></a>
          <nav aria-label="页面导航"><a className="nav-current" href="#ranking">排行榜</a><a href="#models">模型洞察</a><a href="#foundation">关于</a></nav>
          <button className="management-button" disabled title="排名管理尚未开放">排名管理 · 待开放</button>
        </div>
      </header>
      <main id="main" className="page-width">
        <section id="overview" className="overview" aria-labelledby="page-title">
          <div><p className="eyebrow">A CLEARER VIEW OF AI USAGE</p><h1 id="page-title">AI 用量排行榜</h1><p className="page-description">每一次调用，都将在这里被看见。</p></div>
          <dl className="summary" aria-label="用量概览，尚无数据">
            <div><dd>—</dd><dt>参与客户端</dt></div>
            <div><dd className="violet">—</dd><dt>Token 用量</dt></div>
            <div><dd>—</dd><dt>活跃模型</dt></div>
          </dl>
        </section>

        <section id="ranking" className="ranking-section" aria-label="排行榜，等待数据">
          <div className="ranking-toolbar">
            <fieldset className="period-control" disabled><legend className="sr-only">时间范围（尚未开放）</legend><button type="button">今日</button><button type="button">本周</button><button type="button">本月</button></fieldset>
            <div className="filter-controls"><label className="search-control"><span aria-hidden="true">⌕</span><span className="sr-only">搜索参与者（尚未开放）</span><input disabled placeholder="搜索参与者" /></label><label><span className="sr-only">模型筛选（尚未开放）</span><select disabled defaultValue=""><option value="">全部模型</option></select></label></div>
          </div>
          <p className="filter-note">数据尚未接入 · 时间范围、搜索与筛选暂未开放</p>
          <div className="ranking-columns" aria-hidden="true"><span>排名</span><span>参与者</span><span>用量信息</span><span>请求信息</span><span>使用模型</span></div>
          <div className="ranking-empty">
            <div className="empty-symbol" aria-hidden="true"><BrandMark /><span className="empty-symbol-dot" /></div>
            <span className="empty-label">WAITING FOR DATA</span>
            <h2>等待第一份真实数据</h2>
            <p>榜单正在准备中，真实的使用将在这里汇聚。<br />当前尚未开放上报与排名，不显示模拟名次或虚构统计。</p>
            <a href="#status" className="primary-link">查看服务状态<ArrowIcon /></a>
            <span className="empty-caption">排名规则与统计口径将在后续确认</span>
          </div>
          <div className="ranking-bottom"><span><i />尚无排名数据</span><span>真实数据接入后展示</span></div>
        </section>

        <section id="models" className="model-distribution" aria-labelledby="models-heading">
          <h2 id="models-heading">模型分布</h2><div className="distribution-placeholder" aria-hidden="true" /><p>等待模型用量数据<span>暂无分布比例</span></p>
        </section>
        <section id="foundation" className="foundation" aria-labelledby="foundation-heading">
          <div className="foundation-copy"><span className="eyebrow">ABOUT THIS PREVIEW</span><h2 id="foundation-heading">先连接，再看见。</h2><p>这是 CPA Usage Ranking 的界面预览。<br />目前仅支持服务存活检测，注册、上报与排名业务尚未开放。</p><div className="preview-badge">界面预览 <span>/</span> 非正式榜单</div></div>
          <StatusPanel state={health.state} message={health.message} onCheck={health.check} />
        </section>
      </main>
      <footer className="site-footer page-width"><span>CPA Usage Ranking</span><p>等待真实数据，不使用示例统计。</p></footer>
    </div>
  )
}
