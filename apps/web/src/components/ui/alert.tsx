import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Alert({
  className,
  tone = 'default',
  ...props
}: HTMLAttributes<HTMLDivElement> & { tone?: 'default' | 'danger' | 'warning' | 'success' }) {
  const tones = {
    default: 'border-[var(--color-border)] bg-[var(--color-muted)]/40',
    danger: 'border-red-500/40 bg-red-500/10 text-red-800 dark:text-red-200',
    warning: 'border-amber-500/40 bg-amber-500/10 text-amber-900 dark:text-amber-200',
    success: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-900 dark:text-emerald-200',
  }
  return (
    <div
      role="alert"
      className={cn('rounded-md border px-3 py-2 text-sm', tones[tone], className)}
      {...props}
    />
  )
}

export function AlertTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h3 className={cn('font-semibold leading-none tracking-tight', className)} {...props} />
}

export function AlertDescription({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mt-1 text-sm leading-relaxed', className)} {...props} />
}
