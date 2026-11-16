import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { ConfirmDialog } from './ConfirmDialog'

it('focuses cancel, prevents duplicate confirmation and keeps errors in the dialog', async () => {
  let finish!: (error: Error) => void
  const submit = vi.fn(() => new Promise<void>((_resolve, reject) => { finish = reject }))
  const cancel = vi.fn()
  const user = userEvent.setup()
  render(<ConfirmDialog title="确认操作？" onCancel={cancel} onConfirm={submit} />)
  expect(screen.getByRole('button', { name: '取消' })).toHaveFocus()
  await user.click(screen.getByRole('button', { name: '确认' }))
  expect(screen.getByRole('button', { name: '确认' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: '确认' }))
  expect(submit).toHaveBeenCalledTimes(1)
  finish(new Error('请求失败'))
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  await user.click(screen.getByRole('button', { name: '取消' }))
  expect(cancel).toHaveBeenCalledTimes(1)
})
