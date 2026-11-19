import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'
import { DEFAULT_LOCALE, LOCALE_STORAGE_KEY, setLocale } from '../i18n'

if (typeof HTMLDialogElement !== 'undefined' && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  HTMLDialogElement.prototype.close = function () { this.removeAttribute('open') }
}

afterEach(() => {
  cleanup()
  if (typeof window !== 'undefined') {
    window.localStorage.removeItem(LOCALE_STORAGE_KEY)
    setLocale(DEFAULT_LOCALE)
  }
})
