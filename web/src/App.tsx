import { ArrowIcon, BrandMark } from './components/Brand'
import { StatusPanel } from './components/StatusPanel'
import { useHealth } from './hooks/useHealth'
import './styles/app.css'

export default function App() {
  const health = useHealth()
  return (
    <div className="app-shell">
      <div className="ambient-field" aria-hidden="true" />
      <a className="skip-link" href="#main">跳到主要内容</a>
      <header className="site-header page-width">
        <a className="brand" href="#overview" aria-label="CPA Usage Ranking 首页"><BrandMark /><span>CPA<span className="brand-divider">/</span><strong>RANK</strong></span></a>
        <nav aria-label="页面导航"><a href="#overview">概览</a><a href="#status">服务状态</a><a href="#foundation">关于框架</a></nav>
        <span className="build-badge"><span />FOUNDATION</span>
      </header>

      <main id="main" className="page-width" tabIndex={-1}>
        <section id="overview" className="hero" aria-labelledby="hero-title">
          <div className="hero-copy">
            <p className="eyebrow hero-eyebrow"><span className="tiny-line" />THE SIGNAL BEHIND EVERY TOKEN</p>
            <h1 id="hero-title">让每一份用量，<br /><span>都有清晰的坐标。</span></h1>
            <p className="hero-description">把分散的使用，汇成清晰的视野。<br />一个独立、开放接入的通用用量排名服务，正在从这里开始。</p>
            <div className="hero-actions"><a href="#status" className="primary-link">查看服务状态<ArrowIcon /></a><span className="preview-note">基础框架预览 · 非正式榜单</span></div>
          </div>
          <div className="signal-art" aria-hidden="true">
            <div className="orbit orbit--outer" /><div className="orbit orbit--middle" /><div className="orbit orbit--inner" />
            <div className="signal-cross signal-cross--horizontal" /><div className="signal-cross signal-cross--vertical" />
            <div className="orbit-node orbit-node--one" /><div className="orbit-node orbit-node--two" /><div className="orbit-node orbit-node--three" />
            <div className="signal-core"><BrandMark /></div>
            <span className="art-label art-label--top">CLEAR SIGNAL.</span><span className="art-label art-label--bottom">MEANINGFUL USAGE.</span>
          </div>
        </section>

        <div className="workspace-grid">
          <section className="empty-panel glass-panel" aria-labelledby="empty-heading">
            <div className="section-topline"><span className="eyebrow">THE NEXT CHAPTER</span><span className="outline-pill">尚未开放</span></div>
            <div className="empty-visual" aria-hidden="true"><span /><span /><span /><span /><span /><span /><div className="empty-scanline" /></div>
            <div className="empty-copy"><h2 id="empty-heading">等待第一份真实数据</h2><p>这里将呈现真实的用量与排名。<br />当前尚未开放上报与排名，不显示模拟名次或虚构统计。</p></div>
            <div className="empty-footer"><span className="small-dot" />真实数据优先<span className="empty-footer-line" /><span>清晰，而不喧闹</span></div>
          </section>
          <StatusPanel state={health.state} message={health.message} onCheck={health.check} />
        </div>

        <section id="foundation" className="foundation" aria-labelledby="foundation-heading">
          <div className="foundation-heading"><span className="eyebrow">BUILT WITH INTENTION</span><h2 id="foundation-heading">先把基础做好。</h2></div>
          <div className="feature-grid">
            <article><span className="feature-index">01 / INTERFACE</span><h3>有质感，也足够轻。</h3><p>React 与按需视觉组件。没有整套特效库，让每一处光影各有其用。</p></article>
            <article><span className="feature-index">02 / CONNECTION</span><h3>连接真实，状态透明。</h3><p>检测入口与业务状态分开呈现，避免把基础服务误当作完整榜单。</p></article>
            <article><span className="feature-index">03 / FOUNDATION</span><h3>一步一步，稳稳向前。</h3><p>当前仅提供基础框架。注册、存储与排名将在后续功能中逐步接入。</p></article>
          </div>
        </section>
      </main>
      <footer className="site-footer page-width"><span>CPA USAGE RANKING</span><p>让使用被看见，让数据有意义。</p><span className="footer-state">FOUNDATION PREVIEW</span></footer>
    </div>
  )
}
