import { Suspense } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { CommandPalette } from '@/components/layout/CommandPalette'
import { PageFallback } from '@/components/layout/PageFallback'
import { Sidebar } from '@/components/layout/Sidebar'
import { Topbar } from '@/components/layout/Topbar'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import type { HighlandUser } from '@/api/client'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/store/ui'

type AppShellProps = {
  user: HighlandUser | null
  onLogout: () => void
}

export function AppShell({ user, onLogout }: AppShellProps) {
  const location = useLocation()
  const mobileOpen = useUIStore((s) => s.mobileSidebarOpen)
  const setMobileSidebarOpen = useUIStore((s) => s.setMobileSidebarOpen)

  return (
    <div className="flex h-full min-h-0" data-testid="app-shell">
      {/* Desktop sidebar */}
      <div className="hidden h-full md:block">
        <Sidebar />
      </div>

      {/* Mobile drawer */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 flex md:hidden" data-testid="mobile-drawer">
          <button
            type="button"
            className="absolute inset-0 bg-black/40"
            aria-label="Close sidebar"
            onClick={() => setMobileSidebarOpen(false)}
          />
          <div className="relative z-10 h-full shadow-xl">
            <Sidebar />
          </div>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar pathname={location.pathname} search={location.search} user={user} onLogout={onLogout} />
        <main
          className={cn(
            'flex-1 overflow-auto bg-[var(--color-background)] p-4 md:p-8',
          )}
        >
          <div className="mx-auto max-w-[1400px]">
            {/* Reset the boundary on navigation, and show a skeleton while the
                lazily-loaded route chunk streams in — the shell stays put. */}
            <ErrorBoundary resetKey={location.pathname}>
              <Suspense fallback={<PageFallback />}>
                <Outlet />
              </Suspense>
            </ErrorBoundary>
          </div>
        </main>
      </div>

      <CommandPalette role={user?.role} onLogout={onLogout} />
    </div>
  )
}
