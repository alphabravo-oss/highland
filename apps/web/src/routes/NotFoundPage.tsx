import { Link } from 'react-router-dom'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function NotFoundPage() {
  const { t } = useAppTranslation()
  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center gap-3 p-8" data-testid="not-found">
      <h1 className="text-2xl font-semibold">{t('notFound.title')}</h1>
      <p className="text-sm text-[var(--color-muted-foreground)]">
        {t('notFound.description')}
      </p>
      <Link
        to="/dashboard"
        className="inline-flex h-9 items-center rounded-md bg-[var(--color-primary)] px-4 text-sm text-[var(--color-primary-foreground)]"
      >
        {t('notFound.backToDashboard')}
      </Link>
    </div>
  )
}
