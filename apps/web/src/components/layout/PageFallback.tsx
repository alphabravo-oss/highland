import { Skeleton } from '@/components/ui/skeleton'

/**
 * Suspense fallback shown while a lazily-loaded route chunk is fetched. Renders
 * inside the shell's content area, so the nav/topbar stay visible and stable.
 */
export function PageFallback() {
  return (
    <div className="space-y-4" data-testid="page-loading" role="status" aria-busy="true">
      <span className="sr-only">Loading…</span>
      <Skeleton className="h-8 w-64" />
      <Skeleton className="h-4 w-96 max-w-full" />
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-28 w-full" />
      </div>
      <Skeleton className="h-64 w-full" />
    </div>
  )
}
