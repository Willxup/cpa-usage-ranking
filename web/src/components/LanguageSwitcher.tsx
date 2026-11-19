import { useI18n, type Locale } from '../i18n'

const options: ReadonlyArray<{ value: Locale; label: string }> = [
  { value: 'en', label: 'EN' },
  { value: 'zh', label: '中文' },
]

export function LanguageSwitcher() {
  const { locale, setLocale, m } = useI18n()
  return (
    <div className="lang-switch" role="group" aria-label={m.nav.languageGroup}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={locale === option.value}
          aria-label={option.value === 'en' ? m.nav.switchToEn : m.nav.switchToZh}
          onClick={() => setLocale(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}
