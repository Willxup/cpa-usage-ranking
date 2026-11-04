import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import App from '../App'

describe('application shell', () => {
  it('has readable landmarks and identifies unfinished business', () => {
    render(<App />)
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('AI 用量排行榜')
    expect(screen.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
    expect(screen.getByText(/当前尚未开放上报与排名/)).toBeVisible()
    expect(screen.getByText('尚未就绪')).toBeVisible()
    expect(screen.getByRole('link', { name: 'CPA Usage Ranking 首页' })).toHaveAttribute('href', '#overview')
  })

  it('keeps metrics unknown and unimplemented filters disabled', () => {
    render(<App />)
    expect(screen.getAllByText('—')).toHaveLength(3)
    expect(screen.getByRole('textbox', { name: '搜索参与者（尚未开放）' })).toBeDisabled()
    expect(screen.getByRole('combobox', { name: '模型筛选（尚未开放）' })).toBeDisabled()
    for (const name of ['今日', '本周', '本月']) expect(screen.getByRole('button', { name })).toBeDisabled()
    expect(screen.getByText('暂无分布比例')).toBeVisible()
  })

  it('links navigation to real content without placeholder business routes', () => {
    render(<App />)
    const navigation = screen.getByRole('navigation', { name: '页面导航' })
    for (const link of navigation.querySelectorAll('a')) {
      const hash = link.getAttribute('href')!
      expect(hash).toMatch(/^#.+/)
      expect(document.getElementById(hash.slice(1))).not.toBeNull()
    }
    expect(screen.queryByRole('link', { name: '跳到主要内容' })).not.toBeInTheDocument()
  })

  it('connects the visible check button to the health contract without claiming readiness', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"status":"ok"}', {
      headers: { 'Content-Type': 'application/json' },
    })))
    const user = userEvent.setup()
    render(<App />)
    await user.click(screen.getByRole('button', { name: '检查服务连接' }))
    expect(await screen.findByText('服务在线')).toBeVisible()
    expect(screen.getByText('尚未就绪')).toBeVisible()
    expect(screen.getByRole('status')).toHaveTextContent('排名业务尚未接入')
  })
})
