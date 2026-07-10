import type { ReactNode } from 'react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function QueryState({
  isLoading,
  error,
  isEmpty,
  emptyTitle,
  emptyDescription,
  onRetry,
  children,
}: {
  isLoading: boolean
  error: Error | null
  isEmpty?: boolean
  emptyTitle?: string
  emptyDescription?: string
  onRetry?: () => void
  children: ReactNode
}) {
  const { t } = useAppTranslation()
  const resolvedEmptyTitle = emptyTitle ?? t('common.noData')

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16 text-sm text-[var(--color-muted-foreground)]" data-testid="loading">
        {t('common.loading')}
      </div>
    )
  }
  if (error) {
    return (
      <Alert tone="danger" data-testid="error">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span>{error.message}</span>
          {onRetry ? (
            <Button type="button" size="sm" variant="outline" onClick={onRetry}>
              {t('common.retry')}
            </Button>
          ) : null}
        </div>
      </Alert>
    )
  }
  if (isEmpty) {
    return (
      <div
        className="rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] px-6 py-12 text-center"
        data-testid="empty"
      >
        <p className="font-medium">{resolvedEmptyTitle}</p>
        {emptyDescription ? (
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{emptyDescription}</p>
        ) : null}
      </div>
    )
  }
  return <>{children}</>
}
