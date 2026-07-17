import { Database, PanelLeft, PanelLeftClose } from 'lucide-react'
import { HighlandLogo } from '@/components/layout/HighlandLogo'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { HighlandVersionBadge } from '@/components/layout/HighlandVersionBadge'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { useAuth } from '@/auth/AuthContext'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { navigationForWorkspace, providerWorkspaceFromLocation, workspaceLanding } from '@/lib/nav'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/store/ui'
import { useStorageProviders } from '@/api/storage/hooks'
import { prefetchRoute } from '@/lib/routePrefetch'

type SidebarProps = {
  className?: string
}

export function Sidebar({ className }: SidebarProps) {
  const { t } = useAppTranslation()
  const { user } = useAuth()
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const setMobileSidebarOpen = useUIStore((s) => s.setMobileSidebarOpen)
  const providers = useStorageProviders()
  const location = useLocation()
  const navigate = useNavigate()
  const discoveredProviders = providers.data?.data
  const workspace = providerWorkspaceFromLocation(location.pathname, location.search, discoveredProviders)
  const groups = navigationForWorkspace(workspace, user?.role)
  const workspaceOptions = discoveredProviders ? [...discoveredProviders] : workspace ? [workspace] : []
  if (workspace && !workspaceOptions.some((provider) => provider.id === workspace.id)) workspaceOptions.push(workspace)

  const changeWorkspace = (id: string) => {
    const provider = id === '__all__' ? undefined : workspaceOptions.find((candidate) => candidate.id === id)
    navigate(workspaceLanding(provider))
    setMobileSidebarOpen(false)
  }

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
          'justify-center',
        )}
      >
        {!collapsed && (
          <div className="flex items-center gap-2 tracking-tight">
            <HighlandLogo size={24} className="text-[var(--color-primary)]" />
            <div className="flex flex-col leading-tight">
              <span className="font-semibold">{t('app.name')}</span>
              <span className="text-[10px] font-normal text-[var(--color-muted-foreground)]">{t('app.by')}</span>
            </div>
          </div>
        )}
        {collapsed && <HighlandLogo size={24} className="text-[var(--color-primary)]" />}
      </div>

      <div className={cn('border-b border-[var(--color-border)]', collapsed ? 'p-2' : 'p-3')}>
        {collapsed ? (
          <div className="space-y-1">
            <button
              type="button"
              className="flex h-10 w-full items-center justify-center rounded-lg border border-[var(--color-border)] bg-[var(--color-background)] text-[var(--color-primary)] hover:bg-[var(--color-accent)]"
              title={workspace?.displayName ?? t('nav.allStorage', { defaultValue: 'All storage' })}
              aria-label={`${t('nav.workspace', { defaultValue: 'Workspace' })}: ${workspace?.displayName ?? t('nav.allStorage', { defaultValue: 'All storage' })}`}
              onClick={() => navigate(workspaceLanding(workspace))}
            >
              <Database size={18} strokeWidth={1.75} />
            </button>
            <Button type="button" variant="ghost" size="icon" className="hidden w-full md:inline-flex" onClick={toggleSidebar} aria-label={t('nav.expandSidebar')} data-testid="sidebar-collapse">
              <PanelLeft size={18} strokeWidth={1.75} />
            </Button>
          </div>
        ) : (
          <div className="space-y-1.5">
            <label htmlFor="storage-workspace" className="block px-0.5 text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
              {t('nav.workspace', { defaultValue: 'Workspace' })}
            </label>
            <div className="flex items-center gap-1">
              <Select
                id="storage-workspace"
                aria-label={t('nav.storageWorkspace', { defaultValue: 'Storage workspace' })}
                value={workspace?.id ?? '__all__'}
                onChange={(event) => changeWorkspace(event.target.value)}
                className="h-10 min-w-0 flex-1 bg-[var(--color-sidebar)]"
              >
                <option value="__all__">{t('nav.allStorage', { defaultValue: 'All storage' })}</option>
                {workspaceOptions.map((provider) => (
                  <option key={provider.id} value={provider.id}>{provider.displayName}</option>
                ))}
              </Select>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="shrink-0"
                onClick={() => {
                  toggleSidebar()
                  setMobileSidebarOpen(false)
                }}
                aria-label={t('nav.collapseSidebar')}
                data-testid="sidebar-collapse"
              >
                <PanelLeftClose size={18} strokeWidth={1.75} />
              </Button>
            </div>
            <div className="flex items-center justify-between px-0.5 text-[10px] text-[var(--color-muted-foreground)]">
              <span>{workspace ? workspace.kind : t('nav.crossProvider', { defaultValue: 'Cross-provider' })}</span>
              {workspace ? <span>{workspace.supportLevel}</span> : null}
            </div>
          </div>
        )}
      </div>

      <nav className="flex-1 overflow-y-auto px-2 py-3" aria-label={t('nav.main')}>
        {groups.map((group) => {
          const groupLabel = t(group.labelKey, { defaultValue: group.label })
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
                  const label = t(item.labelKey, { defaultValue: item.label })
                  return (
                    <li key={item.id}>
                      <NavLink
                        to={item.path}
                        end={item.end}
                        title={label}
                        onPointerEnter={() => prefetchRoute(item.path)}
                        onFocus={() => prefetchRoute(item.path)}
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

      <HighlandVersionBadge collapsed={collapsed} />

      {!collapsed && (
        <div className="border-t border-[var(--color-border)] px-3 py-2 text-[11px] text-[var(--color-muted-foreground)]">
          <a
            href="https://alphabravo.io"
            target="_blank"
            rel="noreferrer"
            className="hover:text-[var(--color-primary)] hover:underline"
          >
            {t('app.builtBy')}
          </a>
        </div>
      )}
    </aside>
  )
}
