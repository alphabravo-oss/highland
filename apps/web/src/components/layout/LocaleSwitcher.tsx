import { Button } from '@/components/ui/button'
import {
  isAppLocale,
  persistLocale,
  SUPPORTED_LOCALES,
  type AppLocale,
} from '@/i18n'
import { useAppTranslation } from '@/i18n/useAppTranslation'

function resolveLocale(language: string): AppLocale {
  const base = language.split('-')[0] ?? language
  return isAppLocale(base) ? base : 'en'
}

/**
 * Cycles UI language between en / es.
 * Preference is persisted under localStorage key `highland-locale`.
 */
export function LocaleSwitcher() {
  const { t, i18n } = useAppTranslation()
  const current = resolveLocale(i18n.language)

  function cycle() {
    const idx = SUPPORTED_LOCALES.indexOf(current)
    const next = SUPPORTED_LOCALES[(idx + 1) % SUPPORTED_LOCALES.length] ?? 'en'
    void i18n.changeLanguage(next)
    persistLocale(next)
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={cycle}
      aria-label={t('locale.switch', { lng: current.toUpperCase() })}
      title={t('locale.switch', { lng: current.toUpperCase() })}
      data-testid="locale-switcher"
    >
      <span className="text-[11px] font-semibold tracking-wide">{current.toUpperCase()}</span>
    </Button>
  )
}
