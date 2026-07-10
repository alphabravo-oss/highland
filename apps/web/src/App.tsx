import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '@/auth/AuthContext'
import { ToastProvider } from '@/components/ui/toast'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeProvider } from '@/features/theme/ThemeProvider'
import { AuthenticatedLayout } from '@/routes/ProtectedRoute'
import { LoginPage } from '@/routes/LoginPage'
import { DashboardPage } from '@/features/dashboard/DashboardPage'
import { VolumesPage } from '@/features/volumes/VolumesPage'
import { VolumeDetailPage } from '@/features/volumes/VolumeDetailPage'
import { NodesPage } from '@/features/nodes/NodesPage'
import { NodeDetailPage } from '@/features/nodes/NodeDetailPage'
import { BackupsPage } from '@/features/backups/BackupsPage'
import { BackupTargetsPage } from '@/features/backup-targets/BackupTargetsPage'
import { RecurringJobsPage } from '@/features/recurring-jobs/RecurringJobsPage'
import { SystemBackupsPage } from '@/features/system-backups/SystemBackupsPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { SupportBundlePage } from '@/features/support-bundle/SupportBundlePage'
import { EngineImagesPage } from '@/features/engine-images/EngineImagesPage'
import { BackingImagesPage } from '@/features/backing-images/BackingImagesPage'
import { InstanceManagersPage } from '@/features/instance-managers/InstanceManagersPage'
import { OrphansPage } from '@/features/orphans/OrphansPage'
import { PerformancePage, BenchmarksPage } from '@/features/performance/PerformancePage'
import { AdminPage, AuditPage, PreflightPage } from '@/features/admin/AdminPage'
import { SSOConfigPage } from '@/features/admin/SSOConfigPage'
import { StatusPage } from '@/features/status/StatusPage'
import { NotFoundPage } from '@/routes/NotFoundPage'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 5_000,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider delayDuration={300}>
          <ToastProvider>
            <AuthProvider>
              <BrowserRouter>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />
                  <Route element={<AuthenticatedLayout />}>
                    <Route index element={<Navigate to="/dashboard" replace />} />
                    <Route path="dashboard" element={<DashboardPage />} />
                    <Route path="volumes" element={<VolumesPage />} />
                    <Route path="volumes/:name" element={<VolumeDetailPage />} />
                    <Route path="nodes" element={<NodesPage />} />
                    <Route path="nodes/:name" element={<NodeDetailPage />} />
                    <Route path="backups" element={<BackupsPage />} />
                    <Route path="backup-targets" element={<BackupTargetsPage />} />
                    <Route path="recurring-jobs" element={<RecurringJobsPage />} />
                    <Route path="system-backups" element={<SystemBackupsPage />} />
                    <Route path="settings" element={<SettingsPage />} />
                    <Route path="support-bundle" element={<SupportBundlePage />} />
                    <Route path="engine-images" element={<EngineImagesPage />} />
                    <Route path="backing-images" element={<BackingImagesPage />} />
                    <Route path="instance-managers" element={<InstanceManagersPage />} />
                    <Route path="orphans" element={<OrphansPage />} />
                    <Route path="performance" element={<PerformancePage />} />
                    <Route path="benchmarks" element={<BenchmarksPage />} />
                    <Route path="admin" element={<AdminPage />} />
                    <Route path="admin/sso" element={<SSOConfigPage />} />
                    <Route path="admin/audit" element={<AuditPage />} />
                    <Route path="preflight" element={<PreflightPage />} />
                    <Route path="status" element={<StatusPage />} />
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                  <Route path="*" element={<Navigate to="/dashboard" replace />} />
                </Routes>
              </BrowserRouter>
            </AuthProvider>
          </ToastProvider>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
