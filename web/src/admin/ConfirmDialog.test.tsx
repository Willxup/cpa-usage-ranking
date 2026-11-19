import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import { ConfirmDialog } from './ConfirmDialog'

it('focuses cancel, prevents duplicate confirmation and keeps errors in the dialog', async () => {
  let finish!: (error: Error) => void
  const submit = vi.fn(() => new Promise<void>((_resolve, reject) => { finish = reject }))
  const cancel = vi.fn()
  const user = userEvent.setup()
  render(<ConfirmDialog title="Confirm action?" onCancel={cancel} onConfirm={submit} />)
  expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
  await user.click(screen.getByRole('button', { name: 'Confirm' }))
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Confirm' }))
  expect(submit).toHaveBeenCalledTimes(1)
  finish(new Error('Request failed'))
  expect(await screen.findByRole('alert')).toHaveTextContent('Request failed')
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(cancel).toHaveBeenCalledTimes(1)
})

it('renders Chinese dialog actions when locale is zh', () => {
  setLocale('zh')
  render(<ConfirmDialog title="确认操作？" onCancel={vi.fn()} onConfirm={vi.fn()} />)
  expect(screen.getByRole('button', { name: '取消' })).toBeVisible()
  expect(screen.getByRole('button', { name: '确认' })).toBeVisible()
})
