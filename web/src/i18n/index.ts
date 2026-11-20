import { useSyncExternalStore } from 'react'
import { en, zh, type Messages } from './messages'

export type Locale = 'en' | 'zh'
export type { Messages }

export const DEFAULT_LOCALE: Locale = 'en'
export const LOCALE_STORAGE_KEY = 'ranking_locale'

const dictionaries: Record<Locale, Messages> = { en, zh }
const listeners = new Set<() => void>()
let memoryFallback: Locale = DEFAULT_LOCALE
let memoryAuthoritative = false

export function isSupportedLocale(value: unknown): value is Locale {
  return value === 'en' || value === 'zh'
}

function syncDocumentLang(locale: Locale) {
  if (typeof document !== 'undefined') {
    const tag = dictionaries[locale].langTag
    if (document.documentElement.lang !== tag) {
      document.documentElement.lang = tag
    }
  }
}

export function getLocale(): Locale {
  if (!memoryAuthoritative) {
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY)
        const resolved = isSupportedLocale(stored) ? stored : DEFAULT_LOCALE
        memoryFallback = resolved
        syncDocumentLang(resolved)
        return resolved
      }
    } catch {
      // Fall back to in-memory state when storage access is blocked.
    }
  }
  syncDocumentLang(memoryFallback)
  return memoryFallback
}

export function setLocale(next: Locale): void {
  if (!isSupportedLocale(next)) return
  const previous = getLocale()
  memoryFallback = next
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      window.localStorage.setItem(LOCALE_STORAGE_KEY, next)
      memoryAuthoritative = false
    } else {
      memoryAuthoritative = true
    }
  } catch {
    // Keep the in-memory selection if storage writes fail.
    memoryAuthoritative = true
  }
  syncDocumentLang(next)
  if (previous !== next) {
    listeners.forEach((listener) => listener())
  }
}

export function getMessages(locale: Locale = getLocale()): Messages {
  return dictionaries[locale]
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key === LOCALE_STORAGE_KEY) {
      memoryAuthoritative = false
      syncDocumentLang(getLocale())
      listener()
    }
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('storage', onStorage)
  }
  return () => {
    listeners.delete(listener)
    if (typeof window !== 'undefined') {
      window.removeEventListener('storage', onStorage)
    }
  }
}

export function useI18n(): { locale: Locale; setLocale: (next: Locale) => void; m: Messages } {
  const locale = useSyncExternalStore(subscribe, getLocale, () => DEFAULT_LOCALE)
  return { locale, setLocale, m: dictionaries[locale] }
}

export class LocalizedError extends Error {
  readonly format: (messages: Messages) => string
  constructor(format: (messages: Messages) => string) {
    super(format(getMessages()))
    this.name = 'LocalizedError'
    this.format = format
  }
  resolve(locale: Locale = getLocale()): string {
    return this.format(dictionaries[locale])
  }
}

export function formatLocalizedError(
  error: unknown,
  fallback: (messages: Messages) => string,
  locale: Locale = getLocale(),
): string {
  if (error instanceof LocalizedError) return error.resolve(locale)
  if (error instanceof Error && error.message) return error.message
  return fallback(dictionaries[locale])
}

syncDocumentLang(getLocale())
