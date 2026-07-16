import { useAppTranslation } from '@/i18n/useAppTranslation'
import { findNavItem } from '@/lib/nav'

type BreadcrumbsProps = {
  pathname: string
}

export function Breadcrumbs({ pathname }: BreadcrumbsProps) {
  const { t } = useAppTranslation()
  const item = findNavItem(pathname)
  const title = item ? t(item.labelKey, { defaultValue: item.label }) : t('app.name')

  return (
    <nav aria-label="Breadcrumb" className="min-w-0 flex-1" data-testid="breadcrumbs">
      <ol className="flex items-center gap-2 text-sm">
        <li className="truncate font-medium text-[var(--color-foreground)]">{title}</li>
      </ol>
    </nav>
  )
}
