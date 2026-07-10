import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon?: LucideIcon
  title: string
  description?: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center rounded-xl border border-dashed border-[var(--color-border)] bg-[var(--color-card)] px-6 py-14 text-center',
        className,
      )}
      data-testid="empty"
    >
      {Icon ? (
        <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-accent)] text-[var(--color-primary)]">
          <Icon size={22} strokeWidth={1.75} />
        </div>
      ) : null}
      <p className="text-base font-semibold tracking-tight">{title}</p>
      {description ? (
        <p className="mt-1.5 max-w-md text-sm text-[var(--color-muted-foreground)]">
          {description}
        </p>
      ) : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  )
}
