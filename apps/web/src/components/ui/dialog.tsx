import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type DialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  children: ReactNode
  className?: string
  footer?: ReactNode
}

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  className,
  footer,
}: DialogProps) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal>
      <button
        type="button"
        className="absolute inset-0 bg-black/50"
        aria-label="Close dialog"
        onClick={() => onOpenChange(false)}
      />
      <div
        className={cn(
          'relative z-10 w-full max-w-lg rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] shadow-xl',
          className,
        )}
      >
        <div className="flex items-start justify-between gap-3 border-b border-[var(--color-border)] p-4">
          <div>
            <h2 className="text-base font-semibold">{title}</h2>
            {description ? (
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{description}</p>
            ) : null}
          </div>
          <Button type="button" variant="ghost" size="icon" onClick={() => onOpenChange(false)} aria-label="Close">
            <X size={16} strokeWidth={1.75} />
          </Button>
        </div>
        <div className="p-4">{children}</div>
        {footer ? (
          <div className="flex justify-end gap-2 border-t border-[var(--color-border)] p-4">{footer}</div>
        ) : null}
      </div>
    </div>
  )
}
