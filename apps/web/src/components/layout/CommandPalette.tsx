import { useCallback, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { LogOut, Palette } from 'lucide-react'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from '@/components/ui/command'
import { useTheme, type ThemeMode } from '@/features/theme/useTheme'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { filterNavForRole, navGroups } from '@/lib/nav'
import { useUIStore } from '@/store/ui'

export type CommandPaletteProps = {
  /** Current user role for nav filtering (admin | operator | viewer). */
  role?: string
  /** Called when the user selects Log out. */
  onLogout?: () => void
}

export function CommandPalette({ role, onLogout }: CommandPaletteProps) {
  const { t } = useAppTranslation()
  const open = useUIStore((s) => s.commandPaletteOpen)
  const setOpen = useUIStore((s) => s.setCommandPaletteOpen)
  const toggle = useUIStore((s) => s.toggleCommandPalette)

  const navigate = useNavigate()
  const { theme, cycleTheme } = useTheme()

  const groups = useMemo(() => filterNavForRole(navGroups, role), [role])

  const themeLabels: Record<ThemeMode, string> = {
    light: t('theme.light'),
    dark: t('theme.dark'),
    system: t('theme.system'),
  }

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        toggle()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [toggle])

  const run = useCallback(
    (fn: () => void) => {
      setOpen(false)
      fn()
    },
    [setOpen],
  )

  return (
    <CommandDialog open={open} onOpenChange={setOpen} label={t('common.search')}>
      <CommandInput placeholder={`${t('common.search')}…`} autoFocus />
      <CommandList>
        <CommandEmpty>{t('common.empty')}</CommandEmpty>

        {groups.map((group) => {
          const groupLabel = t(group.labelKey)
          return (
            <CommandGroup key={group.id} heading={groupLabel}>
              {group.items.map((item) => {
                const Icon = item.icon
                const label = t(item.labelKey)
                return (
                  <CommandItem
                    key={item.id}
                    value={`${groupLabel} ${label} ${item.path}`}
                    onSelect={() => run(() => navigate(item.path))}
                  >
                    <Icon size={16} strokeWidth={1.75} />
                    <span>{label}</span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          )
        })}

        <CommandSeparator />

        <CommandGroup heading={t('common.actions')}>
          <CommandItem
            value="theme cycle appearance light dark system"
            onSelect={() => run(() => cycleTheme())}
          >
            <Palette size={16} strokeWidth={1.75} />
            <span>{t('theme.cycle', { mode: themeLabels[theme] })}</span>
            <CommandShortcut>{themeLabels[theme]}</CommandShortcut>
          </CommandItem>
          {onLogout ? (
            <CommandItem value="logout sign out" onSelect={() => run(() => onLogout())}>
              <LogOut size={16} strokeWidth={1.75} />
              <span>{t('auth.logout')}</span>
            </CommandItem>
          ) : null}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
