import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { StarButton } from '../StarButton'

describe('StarButton', () => {
  it('uses native button behavior and supports keyboard activation', async () => {
    const user = userEvent.setup()
    const click = vi.fn()
    render(<StarButton onClick={click}>检查服务连接</StarButton>)
    const button = screen.getByRole('button', { name: '检查服务连接' })
    await user.tab()
    expect(button).toHaveFocus()
    await user.keyboard('{Enter}')
    await user.keyboard(' ')
    expect(click).toHaveBeenCalledTimes(2)
  })

  it('does not activate or receive tab focus when disabled', async () => {
    const user = userEvent.setup()
    const click = vi.fn()
    render(<StarButton disabled onClick={click}>连接中</StarButton>)
    const button = screen.getByRole('button', { name: '连接中' })
    await user.click(button)
    await user.tab()
    expect(click).not.toHaveBeenCalled()
    expect(button).not.toHaveFocus()
  })

  it('does not submit a form unless explicitly requested', async () => {
    const user = userEvent.setup()
    const submit = vi.fn((event) => event.preventDefault())
    render(<form onSubmit={submit}><StarButton>默认操作</StarButton><StarButton type="submit">提交</StarButton></form>)
    await user.click(screen.getByRole('button', { name: '默认操作' }))
    expect(submit).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '提交' }))
    expect(submit).toHaveBeenCalledOnce()
  })

  it('preserves accessible state without exposing decorative trails', () => {
    render(<StarButton aria-busy="true" aria-label="重新检查服务">检查</StarButton>)
    const button = screen.getByRole('button', { name: '重新检查服务' })
    expect(button).toHaveAttribute('aria-busy', 'true')
    expect(button).toHaveAccessibleName('重新检查服务')
  })
})
