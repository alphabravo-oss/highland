import { Rows2, Rows3 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { usePreferences, type Density } from '@/store/preferences'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function DensityToggle({ className }: { className?: string }) {
  const { t } = useAppTranslation()
  const density = usePreferences((s) => s.density)
  const setDensity = usePreferences((s) => s.setDensity)

  const options: { value: Density; label: string; Icon: typeof Rows2 }[] = [
    { value: 'comfortable', label: t('tablePrefs.comfortable'), Icon: Rows3 },
    { value: 'compact', label: t('tablePrefs.compact'), Icon: Rows2 },
  ]

  return (
    <div
      className={cn(
        'inline-flex items-center rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-0.5',
        className,
      )}
      role="group"
      aria-label={t('tablePrefs.density')}
      data-testid="density-toggle"
    >
      {options.map(({ value, label, Icon }) => {
        const active = density === value
        return (
          <Button
            key={value}
            type="button"
            size="sm"
            variant={active ? 'secondary' : 'ghost'}
            className={cn('h-7 gap-1.5 px-2 text-xs', active && 'shadow-sm')}
            aria-pressed={active}
            title={label}
            onClick={() => setDensity(value)}
          >
            <Icon size={14} strokeWidth={1.75} />
            <span className="hidden sm:inline">{label}</span>
          </Button>
        )
      })}
    </div>
  )
}
