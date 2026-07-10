import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import es from './locales/es.json'

/** localStorage key for the active UI locale (plan: highland-locale). */
export const LOCALE_STORAGE_KEY = 'highland-locale'

export const SUPPORTED_LOCALES = ['en', 'es'] as const
export type AppLocale = (typeof SUPPORTED_LOCALES)[number]

export function isAppLocale(value: string | null | undefined): value is AppLocale {
  return value === 'en' || value === 'es'
}

function readStoredLocale(): AppLocale {
  if (typeof window === 'undefined') return 'en'
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    if (isAppLocale(stored)) return stored
  } catch {
    // ignore quota / privacy mode
  }
  return 'en'
}

/** Persist locale and update document lang for a11y. */
export function persistLocale(locale: AppLocale): void {
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    // ignore
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = locale
  }
}

void i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    es: { translation: es },
  },
  lng: readStoredLocale(),
  fallbackLng: 'en',
  supportedLngs: [...SUPPORTED_LOCALES],
  interpolation: {
    // React already escapes
    escapeValue: false,
  },
  // Avoid suspense requirements for simple static resources
  react: {
    useSuspense: false,
  },
})

// Keep <html lang> in sync on boot and language changes
if (typeof document !== 'undefined') {
  document.documentElement.lang = i18n.language.startsWith('es') ? 'es' : 'en'
}

i18n.on('languageChanged', (lng) => {
  const locale: AppLocale = isAppLocale(lng) ? lng : 'en'
  persistLocale(locale)
})

export default i18n
