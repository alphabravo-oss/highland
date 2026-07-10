import { useCompatibility } from '@/api/hooks'
import { cn } from '@/lib/utils'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type CompatibilityBadgeProps = {
  collapsed?: boolean
  longhornVersion?: string
  highlandVersion?: string
  className?: string
}

export function CompatibilityBadge({
  collapsed = false,
  longhornVersion,
  highlandVersion,
  className,
}: CompatibilityBadgeProps) {
  const { t } = useAppTranslation()
  const compat = useCompatibility()
  const hVer =
    highlandVersion ??
    (typeof compat.data?.highlandVersion === 'string' ? compat.data.highlandVersion : '0.1.0')
  const lh =
    longhornVersion ??
    (Array.isArray(compat.data?.longhornSupport)
      ? String((compat.data?.longhornSupport as string[])[0] ?? '…')
      : '1.12.x')

  if (collapsed) {
    return (
      <div
        className={cn(
          'px-2 py-2 text-center text-[10px] text-[var(--color-muted-foreground)]',
          className,
        )}
        title={t('compat.title', { lh, hVer })}
      >
        LH
      </div>
    )
  }
  return (
    <div
      className={cn('px-3 py-2 text-xs text-[var(--color-muted-foreground)]', className)}
      data-testid="compat-badge"
    >
      LH {lh} · H {hVer}
    </div>
  )
}
