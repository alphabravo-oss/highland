import { useCompatibility } from '@/api/hooks'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { cn } from '@/lib/utils'

type HighlandVersionBadgeProps = {
  collapsed?: boolean
  highlandVersion?: string
  className?: string
}

function formatVersion(version: string) {
  return version.startsWith('v') ? version : `v${version}`
}

function compactVersion(version: string) {
  const match = version.match(/^v(\d+\.\d+)/)
  return match ? `v${match[1]}` : version
}

export function HighlandVersionBadge({
  collapsed = false,
  highlandVersion,
  className,
}: HighlandVersionBadgeProps) {
  const { t } = useAppTranslation()
  const compatibility = useCompatibility()
  const version = formatVersion(
    highlandVersion ??
      (typeof compatibility.data?.highlandVersion === 'string'
        ? compatibility.data.highlandVersion
        : '0.3.0'),
  )
  const label = `${t('app.name')} ${version}`

  return (
    <div
      className={cn(
        collapsed
          ? 'px-2 py-2 text-center text-[10px] text-[var(--color-muted-foreground)]'
          : 'px-3 py-2 text-xs text-[var(--color-muted-foreground)]',
        className,
      )}
      data-testid="highland-version-badge"
      title={label}
    >
      {collapsed ? compactVersion(version) : label}
    </div>
  )
}
