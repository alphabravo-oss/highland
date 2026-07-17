import en from './locales/en.json'

/** localStorage key for the active UI locale. */
export const LOCALE_STORAGE_KEY = 'highland-locale'

export const SUPPORTED_LOCALES = ['en', 'es'] as const
export type AppLocale = (typeof SUPPORTED_LOCALES)[number]

type TranslationValues = Record<string, unknown> & { defaultValue?: string }
type Listener = () => void

const resources: Partial<Record<AppLocale, Record<string, unknown>>> = { en }
const listeners = new Set<Listener>()

export function isAppLocale(value: string | null | undefined): value is AppLocale {
  return value === 'en' || value === 'es'
}

function readStoredLocale(): AppLocale {
  if (typeof window === 'undefined') return 'en'
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    if (isAppLocale(stored)) return stored
  } catch {
    // Ignore quota and privacy-mode failures.
  }
  return 'en'
}

let activeLocale: AppLocale = readStoredLocale()
if (activeLocale === 'es') {
  void import('./locales/es.json').then(({ default: locale }) => {
    resources.es = locale
    listeners.forEach((listener) => listener())
  })
}

/** Persist locale and update document lang for accessibility. */
export function persistLocale(locale: AppLocale): void {
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    // Ignore quota and privacy-mode failures.
  }
  if (typeof document !== 'undefined') document.documentElement.lang = locale
}

function lookup(locale: AppLocale, key: string): unknown {
  let value: unknown = resources[locale]
  for (const segment of key.split('.')) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
    value = (value as Record<string, unknown>)[segment]
  }
  return value
}

export function translate(key: string, values: TranslationValues = {}): string {
  const translated = lookup(activeLocale, key) ?? lookup('en', key)
  const template = typeof translated === 'string' ? translated : values.defaultValue ?? key
  return template.replace(/\{\{\s*([^{}\s]+)\s*\}\}/g, (_match, token: string) => {
    const value = values[token]
    return value === null || value === undefined ? '' : String(value)
  })
}

export function subscribeLocale(listener: Listener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getLocale(): AppLocale {
  return activeLocale
}

export const i18n = {
  get language(): AppLocale {
    return activeLocale
  },
  async changeLanguage(locale: string): Promise<void> {
    const next = isAppLocale(locale) ? locale : 'en'
    if (next === activeLocale) return
    if (next === 'es' && !resources.es) {
      resources.es = (await import('./locales/es.json')).default
    }
    activeLocale = next
    persistLocale(next)
    listeners.forEach((listener) => listener())
  },
}

if (typeof document !== 'undefined') document.documentElement.lang = activeLocale

export default i18n
