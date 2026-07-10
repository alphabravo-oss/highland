import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryOptions,
} from '@tanstack/react-query'
import { highlandDelete, highlandGet, highlandPost, highlandPut } from './client'
import {
  backupTargetsApi,
  backupVolumesApi,
  backingImagesApi,
  backupBackingImagesApi,
  dashboardApi,
  engineImagesApi,
  eventsApi,
  instanceManagersApi,
  nodesApi,
  orphansApi,
  recurringJobsApi,
  settingsApi,
  supportBundlesApi,
  systemBackupsApi,
  systemRestoresApi,
  tagsApi,
  volumesApi,
  type BackupTarget,
  type BackupVolume,
  type BackingImage,
  type BackupBackingImage,
  type EngineImage,
  type Node,
  type Orphan,
  type RecurringJob,
  type Setting,
  type SystemBackup,
  type Volume,
} from './longhorn'

const poll = { refetchInterval: 10_000 as const }

export function useDashboard() {
  return useQuery({
    queryKey: ['dashboard'],
    queryFn: () => dashboardApi.get(),
    ...poll,
  })
}

export function useEvents() {
  return useQuery({
    queryKey: ['events'],
    queryFn: () => eventsApi.list(),
    refetchInterval: 15_000,
  })
}

export function useVolumes() {
  return useQuery({
    queryKey: ['volumes'],
    queryFn: () => volumesApi.list(),
    ...poll,
  })
}

export function useVolume(name: string | undefined) {
  return useQuery({
    queryKey: ['volumes', name],
    queryFn: () => volumesApi.get(name!),
    enabled: Boolean(name),
    refetchInterval: 5_000,
  })
}

export function useNode(name: string | undefined) {
  return useQuery({
    queryKey: ['nodes', name],
    queryFn: () => nodesApi.get(name!),
    enabled: Boolean(name),
    refetchInterval: 5_000,
  })
}

export function useNodes() {
  return useQuery({
    queryKey: ['nodes'],
    queryFn: () => nodesApi.list(),
    ...poll,
  })
}

export type StatusResponse = {
  highland: { version: string; sessionBackend: string; benchmarkMode: string }
  longhorn: { version: string; namespace: string; managerUrl: string; reachable: boolean; supported: string[] }
  kubernetes: { version: string }
  components: { api: string; managerProxy: string; metricsScraper: string; scrapeError: string }
  vendor: { name: string; url: string; tagline: string }
}

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => highlandGet<StatusResponse>('/status'),
    refetchInterval: 30_000,
  })
}

export function useNodeTags() {
  return useQuery({ queryKey: ['nodetags'], queryFn: () => tagsApi.node(), refetchInterval: 60_000 })
}

export function useDiskTags() {
  return useQuery({ queryKey: ['disktags'], queryFn: () => tagsApi.disk(), refetchInterval: 60_000 })
}

export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => settingsApi.list(),
    refetchInterval: 30_000,
  })
}

export function useBackupVolumes() {
  return useQuery({
    queryKey: ['backupvolumes'],
    queryFn: () => backupVolumesApi.list(),
    ...poll,
  })
}

export function useBackupTargets() {
  return useQuery({
    queryKey: ['backuptargets'],
    queryFn: () => backupTargetsApi.list(),
    ...poll,
  })
}

export function useRecurringJobs() {
  return useQuery({
    queryKey: ['recurringjobs'],
    queryFn: () => recurringJobsApi.list(),
    ...poll,
  })
}

export function useEngineImages() {
  return useQuery({
    queryKey: ['engineimages'],
    queryFn: () => engineImagesApi.list(),
    ...poll,
  })
}

export function useBackingImages() {
  return useQuery({
    queryKey: ['backingimages'],
    queryFn: () => backingImagesApi.list(),
    ...poll,
  })
}

export function useBackupBackingImages() {
  return useQuery({
    queryKey: ['backupbackingimages'],
    queryFn: () => backupBackingImagesApi.list(),
    ...poll,
  })
}

export function useInstanceManagers() {
  return useQuery({
    queryKey: ['instancemanagers'],
    queryFn: () => instanceManagersApi.list(),
    ...poll,
  })
}

export function useOrphans() {
  return useQuery({
    queryKey: ['orphans'],
    queryFn: () => orphansApi.list(),
    ...poll,
  })
}

export function useSystemBackups() {
  return useQuery({
    queryKey: ['systembackups'],
    queryFn: () => systemBackupsApi.list(),
    ...poll,
  })
}

export function useSystemRestores() {
  return useQuery({
    queryKey: ['systemrestores'],
    queryFn: () => systemRestoresApi.list(),
    ...poll,
  })
}

export function useSupportBundles() {
  return useQuery({
    queryKey: ['supportbundles'],
    queryFn: () => supportBundlesApi.list(),
    refetchInterval: 3_000,
  })
}

export function useInvalidate() {
  const qc = useQueryClient()
  return (keys: string[]) => {
    for (const k of keys) {
      void qc.invalidateQueries({ queryKey: [k] })
    }
  }
}

// Mutations
export function useCreateVolume() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => volumesApi.create(body),
    onSuccess: () => inv(['volumes', 'dashboard']),
  })
}

export function useDeleteVolume() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (vol: Volume) => volumesApi.remove(vol),
    onSuccess: () => inv(['volumes', 'dashboard']),
  })
}

export function useVolumeAction() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({
      vol,
      action,
      params,
    }: {
      vol: Volume
      action: string
      params?: Record<string, unknown>
    }) => volumesApi.action(vol, action, params),
    onSuccess: () => inv(['volumes', 'dashboard']),
  })
}

export function useUpdateNode() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({ node, body }: { node: Node; body: Record<string, unknown> }) =>
      nodesApi.update(node, body),
    onSuccess: () => inv(['nodes', 'dashboard']),
  })
}

export function useNodeAction() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({
      node,
      action,
      params,
    }: {
      node: Node
      action: string
      params?: Record<string, unknown>
    }) => nodesApi.action(node, action, params),
    onSuccess: () => inv(['nodes']),
  })
}

export function useUpdateSetting() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({ setting, value }: { setting: Setting; value: string }) =>
      settingsApi.update(setting, value),
    onSuccess: () => inv(['settings']),
  })
}

export function useCreateBackupCredential() {
  return useMutation({
    mutationFn: (body: { name: string; data: Record<string, string> }) =>
      highlandPost('/backup-credential', body),
  })
}

export function useCreateBackupTarget() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => backupTargetsApi.create(body),
    onSuccess: () => inv(['backuptargets']),
  })
}

export function useDeleteBackupTarget() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (bt: BackupTarget) => backupTargetsApi.remove(bt),
    onSuccess: () => inv(['backuptargets']),
  })
}

export function useCreateRecurringJob() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => recurringJobsApi.create(body),
    onSuccess: () => inv(['recurringjobs']),
  })
}

export function useUpdateRecurringJob() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({ job, body }: { job: RecurringJob; body: Record<string, unknown> }) =>
      recurringJobsApi.update(job, body),
    onSuccess: () => inv(['recurringjobs']),
  })
}

export function useDeleteRecurringJob() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (job: RecurringJob) => recurringJobsApi.remove(job),
    onSuccess: () => inv(['recurringjobs']),
  })
}

export function useCreateSupportBundle() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body?: Record<string, unknown>) => supportBundlesApi.create(body ?? {}),
    onSuccess: () => inv(['supportbundles']),
  })
}

export function useDeleteBackupVolume() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (bv: BackupVolume) => backupVolumesApi.remove(bv),
    onSuccess: () => inv(['backupvolumes']),
  })
}

export function useDeleteOrphan() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (o: Orphan) => orphansApi.remove(o),
    onSuccess: () => inv(['orphans']),
  })
}

export function useCreateEngineImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => engineImagesApi.create(body),
    onSuccess: () => inv(['engineimages']),
  })
}

export function useDeleteEngineImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (img: EngineImage) => engineImagesApi.remove(img),
    onSuccess: () => inv(['engineimages']),
  })
}

export function useCreateBackingImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => backingImagesApi.create(body),
    onSuccess: () => inv(['backingimages']),
  })
}

export function useDeleteBackingImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (img: BackingImage) => backingImagesApi.remove(img),
    onSuccess: () => inv(['backingimages']),
  })
}

export function useBackingImageAction() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({
      img,
      action,
      params,
    }: {
      img: BackingImage
      action: string
      params?: Record<string, unknown>
    }) => backingImagesApi.action(img, action, params),
    onSuccess: () => inv(['backingimages', 'backupbackingimages']),
  })
}

export function useBackupBackingImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (img: BackingImage) =>
      backingImagesApi.action(img, 'backupBackingImageCreate'),
    onSuccess: () => inv(['backingimages', 'backupbackingimages']),
  })
}

export function useDeleteBackupBackingImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (bbi: BackupBackingImage) => backupBackingImagesApi.remove(bbi),
    onSuccess: () => inv(['backupbackingimages']),
  })
}

export function useRestoreBackupBackingImage() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({
      bbi,
      params,
    }: {
      bbi: BackupBackingImage
      params?: Record<string, unknown>
    }) => backupBackingImagesApi.action(bbi, 'backupBackingImageRestore', params),
    onSuccess: () => inv(['backingimages', 'backupbackingimages']),
  })
}

export function useCreateSystemBackup() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => systemBackupsApi.create(body),
    onSuccess: () => inv(['systembackups']),
  })
}

export function useDeleteSystemBackup() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (b: SystemBackup) => systemBackupsApi.remove(b),
    onSuccess: () => inv(['systembackups']),
  })
}

export type { UseQueryOptions }

// --- Highland native ---

export function useAuditLog() {
  return useQuery({
    queryKey: ['audit'],
    queryFn: () => highlandGet<{ data: Array<Record<string, unknown>> }>('/audit'),
    refetchInterval: 5_000,
  })
}

export function useHighlandUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: () => highlandGet<{ data: Array<{ username: string; role: string }> }>('/users'),
  })
}

export function useUpdateHighlandUser() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: ({ username, body }: { username: string; body: Record<string, unknown> }) =>
      highlandPut(`/users/${encodeURIComponent(username)}`, body),
    onSuccess: () => inv(['users']),
  })
}

export function useHealthNarrative() {
  return useQuery({
    queryKey: ['health-narrative'],
    queryFn: () => highlandGet<{ items: Array<{ severity: string; code: string; message: string }> }>('/health'),
    refetchInterval: 20_000,
  })
}

export function usePreflight() {
  return useQuery({
    queryKey: ['preflight'],
    queryFn: () => highlandGet<{ checks: Array<{ id: string; name: string; status: string; detail: string }> }>('/preflight'),
  })
}

export function useCapacity() {
  return useQuery({
    queryKey: ['capacity'],
    queryFn: () => highlandGet<{ usedBytes: number; totalBytes: number; note: string; seriesCount: number }>('/capacity'),
    refetchInterval: 15_000,
  })
}

export function useVolumeMetrics(name: string | undefined) {
  return useQuery({
    queryKey: ['volume-metrics', name],
    queryFn: () =>
      highlandGet<{
        series: Array<{ name: string; labels?: Record<string, string>; points: Array<{ t: string; v: number }> }>
        scrapeError?: string
      }>(`/volumes/${encodeURIComponent(name!)}/metrics`),
    enabled: Boolean(name),
    refetchInterval: 10_000,
  })
}

export function useClusterMetrics() {
  return useQuery({
    queryKey: ['metrics'],
    queryFn: () =>
      highlandGet<{
        series: Array<{ name: string; labels?: Record<string, string>; points: Array<{ t: string; v: number }> }>
        scrapeError?: string
      }>('/metrics'),
    refetchInterval: 10_000,
  })
}

export function useBenchmarks() {
  return useQuery({
    queryKey: ['benchmarks'],
    queryFn: () => highlandGet<{ data: Array<Record<string, unknown>> }>('/benchmarks'),
    refetchInterval: 2_000,
  })
}

export function useCreateBenchmark() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => highlandPost('/benchmarks', body),
    onSuccess: () => inv(['benchmarks']),
  })
}

export function useDeleteBenchmark() {
  const inv = useInvalidate()
  return useMutation({
    mutationFn: (name: string) => highlandDelete(`/benchmarks/${encodeURIComponent(name)}`),
    onSuccess: () => inv(['benchmarks']),
  })
}

export function useCompatibility() {
  return useQuery({
    queryKey: ['compatibility'],
    queryFn: () => highlandGet<Record<string, unknown>>('/compatibility'),
  })
}
