import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

// Tone colors tuned for WCAG 2.1 AA (≥4.5:1) on their tinted backgrounds at 12px.
const tones: Record<string, string> = {
  default: 'bg-[var(--color-muted)] text-[var(--color-foreground)]',
  success: 'bg-emerald-600/12 text-emerald-900 dark:bg-emerald-500/20 dark:text-emerald-200',
  warning: 'bg-amber-500/15 text-amber-950 dark:bg-amber-500/20 dark:text-amber-200',
  danger: 'bg-red-500/15 text-red-900 dark:bg-red-500/20 dark:text-red-200',
  info: 'bg-sky-500/15 text-sky-950 dark:bg-sky-500/20 dark:text-sky-200',
  primary: 'bg-[var(--color-primary)]/12 text-[var(--color-primary)] font-semibold',
}

export function Badge({
  className,
  tone = 'default',
  ...props
}: HTMLAttributes<HTMLSpanElement> & { tone?: keyof typeof tones }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
        tones[tone] ?? tones.default,
        className,
      )}
      {...props}
    />
  )
}

export function stateTone(state?: string): keyof typeof tones {
  const s = (state ?? '').toLowerCase()
  if (['healthy', 'ready', 'attached', 'running', 'completed', 'available', 'true'].includes(s))
    return 'success'
  if (['degraded', 'detached', 'pending', 'deploying', 'inprogress', 'warning'].includes(s))
    return 'warning'
  if (['faulted', 'error', 'failed', 'unknown', 'false'].includes(s)) return 'danger'
  return 'default'
}
