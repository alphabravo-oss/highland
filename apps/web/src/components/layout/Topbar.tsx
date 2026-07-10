import { LifeBuoy, Menu, Search } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
import { LocaleSwitcher } from '@/components/layout/LocaleSwitcher'
import { ThemeToggle } from '@/components/layout/ThemeToggle'
import { UserMenu } from '@/components/layout/UserMenu'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { HighlandUser } from '@/api/client'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { useUIStore } from '@/store/ui'

type TopbarProps = {
  pathname: string
  user: HighlandUser | null
  onLogout: () => void
}

export function Topbar({ pathname, user, onLogout }: TopbarProps) {
  const { t } = useAppTranslation()
  const setMobileSidebarOpen = useUIStore((s) => s.setMobileSidebarOpen)
  const setCommandPaletteOpen = useUIStore((s) => s.setCommandPaletteOpen)

  return (
    <header
      className="flex h-14 shrink-0 items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-card)] px-3"
      data-testid="topbar"
    >
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="md:hidden"
        onClick={() => setMobileSidebarOpen(true)}
        aria-label={t('nav.openSidebar')}
        data-testid="mobile-sidebar-toggle"
      >
        <Menu size={18} strokeWidth={1.75} />
      </Button>

      <Breadcrumbs pathname={pathname} />

      <div className="ml-auto flex items-center gap-1">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="hidden gap-1.5 text-[var(--color-muted-foreground)] sm:inline-flex"
              onClick={() => setCommandPaletteOpen(true)}
              data-testid="command-palette-trigger"
            >
              <Search size={14} strokeWidth={1.75} />
              <span className="text-xs">{t('common.search')}</span>
              <kbd className="pointer-events-none ml-1 hidden rounded border border-[var(--color-border)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--color-muted-foreground)] lg:inline">
                ⌘K
              </kbd>
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {t('common.search')} (⌘K)
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Link
              to="/support-bundle"
              aria-label={t('nav.supportBundle')}
              className="inline-flex h-9 w-9 items-center justify-center rounded-md hover:bg-[var(--color-accent)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
            >
              <LifeBuoy size={18} strokeWidth={1.75} />
            </Link>
          </TooltipTrigger>
          <TooltipContent>{t('nav.supportBundle')}</TooltipContent>
        </Tooltip>
        <LocaleSwitcher />
        <ThemeToggle />
        <UserMenu user={user} onLogout={onLogout} />
      </div>
    </header>
  )
}
