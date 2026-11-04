import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { StatusPanel } from '../StatusPanel'

describe('StatusPanel', () => {
  it('shows an honest unchecked state when no request has run', () => {
    render(<StatusPanel state="idle" />)
    expect(screen.getByRole('status')).toHaveTextContent('尚未检测')
    expect(screen.getByRole('button', { name: '检查服务连接' })).toBeDisabled()
    expect(screen.getByText('尚未就绪')).toBeVisible()
  })

  it('prevents duplicate checks while loading', () => {
    render(<StatusPanel state="loading" onCheck={vi.fn()} />)
    expect(screen.getByRole('status')).toHaveTextContent('正在连接')
    expect(screen.getByRole('button', { name: '正在检查…' })).toBeDisabled()
    expect(screen.getByRole('button')).toHaveAttribute('aria-busy', 'true')
  })

  it('does not confuse liveness success with business readiness', () => {
    render(<StatusPanel state="online" onCheck={vi.fn()} />)
    expect(screen.getByRole('status')).toHaveTextContent('服务在线')
    expect(screen.getByRole('status')).toHaveTextContent('排名业务尚未接入')
    expect(screen.getByText('尚未就绪')).toBeVisible()
    expect(screen.getByText('暂未接入')).toBeVisible()
  })

  it('shows a retry action and a useful failure reason', async () => {
    const user = userEvent.setup()
    const retry = vi.fn()
    render(<StatusPanel state="error" message="连接超时，请重试。" onCheck={retry} />)
    expect(screen.getByRole('status')).toHaveTextContent('连接超时，请重试。')
    await user.click(screen.getByRole('button', { name: '检查服务连接' }))
    expect(retry).toHaveBeenCalledOnce()
  })
})
