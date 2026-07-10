import { Mountain, PanelLeft, PanelLeftClose } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import { CompatibilityBadge } from '@/components/layout/CompatibilityBadge'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/auth/AuthContext'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { filterNavForRole, navGroups } from '@/lib/nav'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/store/ui'

type SidebarProps = {
  className?: string
}

export function Sidebar({ className }: SidebarProps) {
  const { t } = useAppTranslation()
  const { user } = useAuth()
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const setMobileSidebarOpen = useUIStore((s) => s.setMobileSidebarOpen)
  const groups = filterNavForRole(navGroups, user?.role)

  return (
    <aside
      className={cn(
        'flex h-full flex-col border-r border-[var(--color-border)] bg-[var(--color-sidebar)] text-[var(--color-sidebar-foreground)] transition-[width] duration-200',
        collapsed ? 'w-16' : 'w-60',
        className,
      )}
      data-testid="sidebar"
      data-collapsed={collapsed ? 'true' : 'false'}
    >
      <div
        className={cn(
          'flex h-14 items-center border-b border-[var(--color-border)] px-3',
          collapsed ? 'justify-center' : 'justify-between gap-2',
        )}
      >
        {!collapsed && (
          <div className="flex items-center gap-2 font-semibold tracking-tight">
            <Mountain size={20} strokeWidth={1.75} className="text-[var(--color-primary)]" />
            <span>{t('app.name')}</span>
          </div>
        )}
        {collapsed && (
          <Mountain size={20} strokeWidth={1.75} className="text-[var(--color-primary)]" />
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className={cn(collapsed && 'hidden md:inline-flex')}
          onClick={() => {
            toggleSidebar()
            setMobileSidebarOpen(false)
          }}
          aria-label={collapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')}
          data-testid="sidebar-collapse"
        >
          {collapsed ? (
            <PanelLeft size={18} strokeWidth={1.75} />
          ) : (
            <PanelLeftClose size={18} strokeWidth={1.75} />
          )}
        </Button>
      </div>

      <nav className="flex-1 overflow-y-auto px-2 py-3" aria-label={t('nav.main')}>
        {groups.map((group) => {
          const groupLabel = t(group.labelKey)
          return (
            <div key={group.id} className="mb-4">
              {!collapsed && (
                <div className="mb-1.5 px-2.5 text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
                  {groupLabel}
                </div>
              )}
              <ul className="space-y-0.5">
                {group.items.map((item) => {
                  const Icon = item.icon
                  const label = t(item.labelKey)
                  return (
                    <li key={item.id}>
                      <NavLink
                        to={item.path}
                        title={label}
                        onClick={() => setMobileSidebarOpen(false)}
                        className={({ isActive }) =>
                          cn(
                            'flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]',
                            collapsed && 'justify-center px-0',
                            isActive
                              ? 'bg-[var(--color-sidebar-active)] font-medium text-[var(--color-primary)] shadow-[var(--shadow-sm)]'
                              : 'text-[var(--color-sidebar-foreground)] hover:bg-[var(--color-accent)]',
                          )
                        }
                      >
                        <Icon size={18} strokeWidth={1.75} aria-hidden />
                        {!collapsed && <span className="truncate">{label}</span>}
                      </NavLink>
                    </li>
                  )
                })}
              </ul>
            </div>
          )
        })}
      </nav>

      <CompatibilityBadge collapsed={collapsed} />
    </aside>
  )
}
