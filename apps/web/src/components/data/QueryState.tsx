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
  emptyAction,
  onRetry,
  skeleton,
  isFetching,
  observedAt,
  stale,
  partial,
  children,
}: {
  isLoading: boolean
  error: Error | null
  isEmpty?: boolean
  emptyTitle?: string
  emptyDescription?: string
  emptyAction?: ReactNode
  onRetry?: () => void
  skeleton?: ReactNode
  isFetching?: boolean
  observedAt?: string
  stale?: boolean
  partial?: boolean
  children: ReactNode
}) {
  const { t } = useAppTranslation()
  const resolvedEmptyTitle = emptyTitle ?? t('common.noData')

  if (isLoading) {
    if (skeleton) {
      return <div data-testid="loading">{skeleton}</div>
    }
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
        {emptyAction ? <div className="mt-4 flex justify-center">{emptyAction}</div> : null}
      </div>
    )
  }
  return (
    <>
      {(isFetching || observedAt || stale || partial) ? (
        <div className="mb-2 flex min-h-5 flex-wrap items-center justify-end gap-2 text-xs text-[var(--color-muted-foreground)]" role="status" aria-live="polite">
          {partial ? <span className="text-amber-700 dark:text-amber-300">Partial results</span> : null}
          {stale ? <span className="text-amber-700 dark:text-amber-300">Cached data may be stale</span> : null}
          {observedAt ? <span>Observed {new Date(observedAt).toLocaleString()}</span> : null}
          {isFetching ? <span className="inline-flex items-center gap-1"><span className="h-2 w-2 animate-pulse rounded-full bg-[var(--color-primary)]" />Updating</span> : null}
        </div>
      ) : null}
      {children}
    </>
  )
}
