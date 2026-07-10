import { Monitor, Moon, Sun } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useTheme, type ThemeMode } from '@/features/theme/useTheme'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const icons: Record<ThemeMode, typeof Sun> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
}

export function ThemeToggle() {
  const { t } = useAppTranslation()
  const { theme, cycleTheme } = useTheme()
  const Icon = icons[theme]
  const modeLabel = t(`theme.${theme}`)
  const aria = t('theme.cycle', { mode: modeLabel })

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={cycleTheme}
      aria-label={aria}
      title={aria}
      data-testid="theme-toggle"
    >
      <Icon size={18} strokeWidth={1.75} />
    </Button>
  )
}
