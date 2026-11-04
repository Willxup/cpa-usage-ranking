import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from '../App'

describe('application shell', () => {
  it('has readable landmarks and identifies unfinished business', () => {
    render(<App />)
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('让每一份用量')
    expect(screen.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
    expect(screen.getByText(/当前尚未开放上报与排名/)).toBeVisible()
    expect(screen.getByText('尚未就绪')).toBeVisible()
    expect(screen.getByRole('link', { name: 'CPA Usage Ranking 首页' })).toHaveAttribute('href', '#overview')
  })

  it('links navigation to real content without placeholder business routes', () => {
    render(<App />)
    const navigation = screen.getByRole('navigation', { name: '页面导航' })
    for (const link of navigation.querySelectorAll('a')) {
      const hash = link.getAttribute('href')!
      expect(hash).toMatch(/^#.+/)
      expect(document.getElementById(hash.slice(1))).not.toBeNull()
    }
    expect(screen.getByRole('link', { name: '跳到主要内容' })).toHaveAttribute('href', '#main')
  })
})
