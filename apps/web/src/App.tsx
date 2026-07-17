import { lazy } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '@/auth/AuthContext'
import { RealtimeProvider } from '@/api/realtime'
import { ToastProvider } from '@/components/ui/toast'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeProvider } from '@/features/theme/ThemeProvider'
// Eager: the shell, the first-view login, and the tiny 404 stay in the initial
// bundle. Every feature page below is code-split and streamed on demand, which
// keeps heavy deps (e.g. recharts) off the initial load.
import { AuthenticatedLayout } from '@/routes/ProtectedRoute'
import { LoginPage } from '@/routes/LoginPage'
import { NotFoundPage } from '@/routes/NotFoundPage'
import { useStorageProviders } from '@/api/storage/hooks'

const DashboardPage = lazy(() =>
  import('@/features/dashboard/DashboardPage').then((m) => ({ default: m.DashboardPage })),
)
const VolumesPage = lazy(() =>
  import('@/features/volumes/VolumesPage').then((m) => ({ default: m.VolumesPage })),
)
const VolumeDetailPage = lazy(() =>
  import('@/features/volumes/VolumeDetailPage').then((m) => ({ default: m.VolumeDetailPage })),
)
const NodesPage = lazy(() =>
  import('@/features/nodes/NodesPage').then((m) => ({ default: m.NodesPage })),
)
const NodeDetailPage = lazy(() =>
  import('@/features/nodes/NodeDetailPage').then((m) => ({ default: m.NodeDetailPage })),
)
const BackupsPage = lazy(() =>
  import('@/features/backups/BackupsPage').then((m) => ({ default: m.BackupsPage })),
)
const BackupTargetsPage = lazy(() =>
  import('@/features/backup-targets/BackupTargetsPage').then((m) => ({ default: m.BackupTargetsPage })),
)
const RecurringJobsPage = lazy(() =>
  import('@/features/recurring-jobs/RecurringJobsPage').then((m) => ({ default: m.RecurringJobsPage })),
)
const SystemBackupsPage = lazy(() =>
  import('@/features/system-backups/SystemBackupsPage').then((m) => ({ default: m.SystemBackupsPage })),
)
const SettingsPage = lazy(() =>
  import('@/features/settings/SettingsPage').then((m) => ({ default: m.SettingsPage })),
)
const SupportBundlePage = lazy(() =>
  import('@/features/support-bundle/SupportBundlePage').then((m) => ({ default: m.SupportBundlePage })),
)
const EngineImagesPage = lazy(() =>
  import('@/features/engine-images/EngineImagesPage').then((m) => ({ default: m.EngineImagesPage })),
)
const BackingImagesPage = lazy(() =>
  import('@/features/backing-images/BackingImagesPage').then((m) => ({ default: m.BackingImagesPage })),
)
const InstanceManagersPage = lazy(() =>
  import('@/features/instance-managers/InstanceManagersPage').then((m) => ({ default: m.InstanceManagersPage })),
)
const OrphansPage = lazy(() =>
  import('@/features/orphans/OrphansPage').then((m) => ({ default: m.OrphansPage })),
)
const PerformancePage = lazy(() =>
  import('@/features/performance/LiveIOPage').then((m) => ({ default: m.LiveIOPage })),
)
const BenchmarksPage = lazy(() =>
  import('@/features/performance/PerformancePage').then((m) => ({ default: m.BenchmarksPage })),
)
const AdminPage = lazy(() =>
  import('@/features/admin/AdminPage').then((m) => ({ default: m.AdminPage })),
)
const AdminUsersPage = lazy(() =>
  import('@/features/admin/AdminPage').then((m) => ({ default: m.AdminUsersPage })),
)
const AuditPage = lazy(() =>
  import('@/features/admin/AdminPage').then((m) => ({ default: m.AuditPage })),
)
const PreflightPage = lazy(() =>
  import('@/features/admin/AdminPage').then((m) => ({ default: m.PreflightPage })),
)
const SSOConfigPage = lazy(() =>
  import('@/features/admin/SSOConfigPage').then((m) => ({ default: m.SSOConfigPage })),
)
const StoragePolicyPage = lazy(() =>
  import('@/features/admin/StoragePolicyPage').then((m) => ({ default: m.StoragePolicyPage })),
)
const StatusPage = lazy(() =>
  import('@/features/status/StatusPage').then((m) => ({ default: m.StatusPage })),
)
const StorageProvidersPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageProvidersPage })))
const StorageProviderPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageProviderPage })))
const StorageInventoryPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageInventoryPage })))
const StorageClassesPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageClassesPage })))
const StorageClaimsPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageClaimsPage })))
const StorageClaimDetailPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageClaimDetailPage })))
const StorageVolumesPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageVolumesPage })))
const StorageVolumeDetailPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageVolumeDetailPage })))
const StorageSnapshotsPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageSnapshotsPage })))
const StorageAttachmentsPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageAttachmentsPage })))
const StorageCapacityPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageCapacityPage })))
const StorageEventsPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.StorageEventsPage })))
const CephResourcePage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.CephResourcePage })))
const CephResourceDetailPage = lazy(() => import('@/features/storage/StoragePages').then((m) => ({ default: m.CephResourceDetailPage })))
const OpenEBSResourcePage = lazy(() => import('@/features/storage/OpenEBSStoragePages').then((m) => ({ default: m.OpenEBSResourcePage })))
const OpenEBSResourceDetailPage = lazy(() => import('@/features/storage/OpenEBSStoragePages').then((m) => ({ default: m.OpenEBSResourceDetailPage })))
const LinstorResourcePage = lazy(() => import('@/features/storage/LinstorStoragePages').then((m) => ({ default: m.LinstorResourcePage })))
const LinstorResourceDetailPage = lazy(() => import('@/features/storage/LinstorStoragePages').then((m) => ({ default: m.LinstorResourceDetailPage })))
const StorageOperationsPage = lazy(() => import('@/features/storage/StorageOperationsPage').then((m) => ({ default: m.StorageOperationsPage })))
const StorageActionPage = lazy(() => import('@/features/storage/StorageOperationsPage').then((m) => ({ default: m.StorageActionPage })))
const StorageOperationDetailPage = lazy(() => import('@/features/storage/StorageOperationsPage').then((m) => ({ default: m.StorageOperationDetailPage })))
const StorageInsightsPage = lazy(() => import('@/features/storage/StorageContextPages').then((m) => ({ default: m.StorageInsightsPage })))
const ProviderContextPage = lazy(() => import('@/features/storage/StorageContextPages').then((m) => ({ default: m.ProviderContextPage })))
const ResourceRelationshipsPage = lazy(() => import('@/features/storage/StorageContextPages').then((m) => ({ default: m.ResourceRelationshipsPage })))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 5_000,
    },
  },
})

function ProviderAwareDashboard() {
  const providers = useStorageProviders()
  if (providers.isLoading) {
    return <div role="status" className="p-6 text-sm text-[var(--color-muted-foreground)]">Loading storage providers…</div>
  }
  if (providers.data && !providers.data.data.some((provider) => provider.id === 'longhorn')) {
    return <StorageProvidersPage />
  }
  return <DashboardPage />
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider delayDuration={300}>
          <ToastProvider>
            <AuthProvider>
              <RealtimeProvider>
              <BrowserRouter>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />
                  <Route element={<AuthenticatedLayout />}>
                    <Route index element={<Navigate to="/dashboard" replace />} />
                    <Route path="dashboard" element={<ProviderAwareDashboard />} />
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
                    <Route path="admin/users" element={<AdminUsersPage />} />
                    <Route path="admin/sso" element={<SSOConfigPage />} />
                    <Route path="admin/audit" element={<AuditPage />} />
                    <Route path="admin/storage-policy" element={<StoragePolicyPage />} />
                    <Route path="preflight" element={<PreflightPage />} />
                    <Route path="status" element={<StatusPage />} />
                    <Route path="storage/providers" element={<StorageProvidersPage />} />
                    <Route path="storage/providers/:providerId" element={<StorageProviderPage />} />
                    <Route path="storage/providers/:providerId/context" element={<ProviderContextPage />} />
                    <Route path="storage/providers/:providerId/ceph/:kind" element={<CephResourcePage />} />
                    <Route path="storage/providers/:providerId/ceph/:kind/:resourceId" element={<CephResourceDetailPage />} />
                    <Route path="storage/providers/:providerId/openebs/:kind" element={<OpenEBSResourcePage />} />
                    <Route path="storage/providers/:providerId/openebs/:kind/:resourceId" element={<OpenEBSResourceDetailPage />} />
                    <Route path="storage/providers/:providerId/linstor/:kind" element={<LinstorResourcePage />} />
                    <Route path="storage/providers/:providerId/linstor/:kind/:resourceId" element={<LinstorResourceDetailPage />} />
                    <Route path="storage/inventory" element={<StorageInventoryPage />} />
                    <Route path="storage/classes" element={<StorageClassesPage />} />
                    <Route path="storage/claims" element={<StorageClaimsPage />} />
                    <Route path="storage/claims/:namespace/:name" element={<StorageClaimDetailPage />} />
                    <Route path="storage/volumes" element={<StorageVolumesPage />} />
                    <Route path="storage/volumes/:name" element={<StorageVolumeDetailPage />} />
                    <Route path="storage/snapshots" element={<StorageSnapshotsPage />} />
                    <Route path="storage/attachments" element={<StorageAttachmentsPage />} />
                    <Route path="storage/capacity" element={<StorageCapacityPage />} />
                    <Route path="storage/events" element={<StorageEventsPage />} />
                    <Route path="storage/insights" element={<StorageInsightsPage />} />
                    <Route path="storage/relationships/:kind/:resourceId" element={<ResourceRelationshipsPage />} />
                    <Route path="storage/operations" element={<StorageOperationsPage />} />
                    <Route path="storage/operations/:operationId" element={<StorageOperationDetailPage />} />
                    <Route path="storage/actions/:actionId" element={<StorageActionPage />} />
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                  <Route path="*" element={<Navigate to="/dashboard" replace />} />
                </Routes>
              </BrowserRouter>
              </RealtimeProvider>
            </AuthProvider>
          </ToastProvider>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
