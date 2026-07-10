import { findNavItem } from '@/lib/nav'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type PlaceholderPageProps = {
  title?: string
  description?: string
}

export function PlaceholderPage({ title, description }: PlaceholderPageProps) {
  const { t } = useAppTranslation()
  const path =
    typeof window !== 'undefined' ? window.location.pathname : '/dashboard'
  const item = findNavItem(path)
  const heading = title ?? item?.label ?? t('placeholder.page')
  const Icon = item?.icon

  return (
    <div className="space-y-4" data-testid="placeholder-page">
      <div className="flex items-center gap-3">
        {Icon ? (
          <Icon size={24} strokeWidth={1.75} className="text-[var(--color-primary)]" />
        ) : null}
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{heading}</h1>
          <p className="text-sm text-[var(--color-muted-foreground)]">
            {description ?? t('placeholder.description')}
          </p>
        </div>
      </div>
      <div className="rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-8 text-sm text-[var(--color-muted-foreground)]">
        {t('placeholder.contentHint', { heading })}
      </div>
    </div>
  )
}
