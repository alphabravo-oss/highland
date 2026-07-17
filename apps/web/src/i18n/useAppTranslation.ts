import { useCallback, useSyncExternalStore } from 'react'
import { getLocale, i18n, subscribeLocale, translate } from './index'

export function useAppTranslation() {
  const locale = useSyncExternalStore(subscribeLocale, getLocale, getLocale)
  const t = useCallback(
    (key: string, values?: Record<string, unknown> & { defaultValue?: string }) => translate(key, values),
    [locale],
  )
  return { t, i18n }
}
