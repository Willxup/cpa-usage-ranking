import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'
import { LanguageSwitcher } from '../../components/LanguageSwitcher'
import { DEFAULT_LOCALE, formatLocalizedError, getLocale, LOCALE_STORAGE_KEY, LocalizedError, setLocale, useI18n } from '../index'

function Probe() {
  const { locale, m } = useI18n()
  return (
    <div>
      <LanguageSwitcher />
      <p data-testid="locale">{locale}</p>
      <h1>{m.publicPage.title}</h1>
    </div>
  )
}

afterEach(() => {
  window.localStorage.clear()
  setLocale(DEFAULT_LOCALE)
})

it('defaults to English, syncs html lang, and persists language toggles', async () => {
  window.localStorage.clear()
  const user = userEvent.setup()
  render(<Probe />)

  expect(getLocale()).toBe('en')
  expect(document.documentElement.lang).toBe('en')
  expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('CPA Usage')
  expect(screen.getByRole('button', { name: 'Switch language to English' })).toHaveAttribute('aria-pressed', 'true')

  await user.click(screen.getByRole('button', { name: 'Switch language to Chinese' }))
  expect(getLocale()).toBe('zh')
  expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh')
  expect(document.documentElement.lang).toBe('zh-CN')
  expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('CPA 用量榜')
  expect(screen.getByRole('button', { name: '切换为中文' })).toHaveAttribute('aria-pressed', 'true')
})

it('falls back to English on unknown stored values and re-translates LocalizedError', () => {
  window.localStorage.setItem(LOCALE_STORAGE_KEY, 'fr')
  expect(getLocale()).toBe('en')

  const err = new LocalizedError((m) => m.leaderboardErrors.notReady)
  expect(err.message).toBe('Leaderboard cache is not ready yet. Please retry shortly.')
  expect(formatLocalizedError(err, (m) => m.leaderboardErrors.generic, 'zh')).toBe('榜单缓存尚未就绪，请稍后重试。')
})
