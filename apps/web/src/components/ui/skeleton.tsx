import { cn } from '@/lib/utils'

/** A shimmering placeholder block used while data loads. */
export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn('animate-pulse rounded-md bg-[var(--color-muted)]', className)}
      aria-hidden
    />
  )
}

/**
 * A table-shaped loading placeholder: a header strip plus `rows` shimmer rows.
 * Keeps list pages from collapsing to a bare spinner while the query resolves.
 */
export function TableSkeleton({ rows = 6, cols = 5 }: { rows?: number; cols?: number }) {
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)]" data-testid="table-skeleton">
      <div className="flex gap-4 border-b border-[var(--color-border)] bg-[var(--color-muted)]/40 px-4 py-3">
        {Array.from({ length: cols }).map((_, i) => (
          <Skeleton key={i} className="h-4 flex-1" />
        ))}
      </div>
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="flex gap-4 border-b border-[var(--color-border)] px-4 py-3 last:border-0">
          {Array.from({ length: cols }).map((_, c) => (
            <Skeleton key={c} className={cn('h-4 flex-1', c === 0 && 'max-w-[40%]')} />
          ))}
        </div>
      ))}
    </div>
  )
}
