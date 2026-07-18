import {
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { highlandDelete, highlandGet, highlandPost, highlandPut } from './client'
import { optimisticPatch, optimisticRemove, pick } from './optimistic'
import { useSseConnected } from './realtime'
import { storagePolicyClient, type PolicyChangeRequest } from './policy'
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
  type LonghornVolume,
} from './longhorn'

// Adaptive polling: when the SSE change stream is healthy, fall back to a slow
// safety poll; when it's down, poll at the fast interval so freshness never
// depends on the stream. Only use for hooks whose keys the watch hub invalidates
// (see internal/watch/hub.go) — never for Highland-native / Prometheus data.
function usePoll(fast = 10_000, slow = 60_000): { refetchInterval: number } {
  return { refetchInterval: useSseConnected() ? slow : fast }
}

export function useStoragePolicy(enabled = true) {
  return useQuery({
    queryKey: ['admin-storage-policy'],
    queryFn: ({ signal }) => storagePolicyClient.get(signal),
    enabled,
    refetchInterval: useSseConnected() ? 60_000 : 15_000,
  })
}

export function useStoragePolicyHistory(enabled = true) {
  return useQuery({
    queryKey: ['admin-storage-policy-history'],
    queryFn: ({ signal }) => storagePolicyClient.history(signal),
    enabled,
  })
}

export function usePlanStoragePolicy() {
  return useMutation({ mutationFn: (request: PolicyChangeRequest) => storagePolicyClient.plan(request) })
}

export function useApplyStoragePolicy() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (request: PolicyChangeRequest) => storagePolicyClient.apply(request),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['admin-storage-policy'] }),
        client.invalidateQueries({ queryKey: ['admin-storage-policy-history'] }),
        client.invalidateQueries({ queryKey: ['storage'] }),
      ])
    },
  })
}

export function useDashboard() {
  return useQuery({
    queryKey: ['dashboard'],
    queryFn: () => dashboardApi.get(),
    ...usePoll(),
  })
}

export function useEvents() {
  return useQuery({
    queryKey: ['events'],
    queryFn: () => eventsApi.list(),
    ...usePoll(15_000, 60_000),
  })
}

export function useVolumes(enabled = true) {
  return useQuery({
    queryKey: ['volumes'],
    queryFn: () => volumesApi.list(),
    enabled,
    ...usePoll(),
  })
}

export function useVolume(name: string | undefined) {
  return useQuery({
    queryKey: ['volumes', name],
    queryFn: () => volumesApi.get(name!),
    enabled: Boolean(name),
    ...usePoll(5_000, 30_000),
  })
}

export function useNode(name: string | undefined) {
  return useQuery({
    queryKey: ['nodes', name],
    queryFn: () => nodesApi.get(name!),
    enabled: Boolean(name),
    ...usePoll(5_000, 30_000),
  })
}

export function useNodes(enabled = true) {
  return useQuery({
    queryKey: ['nodes'],
    queryFn: () => nodesApi.list(),
    enabled,
    ...usePoll(),
  })
}

export type StatusResponse = {
  highland: { version: string; sessionBackend: string; benchmarkMode: string }
  longhorn: { enabled: boolean; version: string; namespace: string; managerUrl: string; reachable: boolean; supported: string[] }
  kubernetes: { version: string }
  components: { api: string; managerProxy: string; metricsScraper: string; scrapeError: string }
  vendor: { name: string; url: string; tagline: string }
  storage?: { ready: boolean; lastSync?: string; snapshotApi?: boolean; providers?: Array<{ id: string; displayName: string; supportLevel: string; health: { status: string } }> }
}

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: ({ signal }) => highlandGet<StatusResponse>('/status', { signal }),
    refetchInterval: 30_000,
  })
}

export function useNodeTags(enabled = true) {
  return useQuery({ queryKey: ['nodetags'], queryFn: () => tagsApi.node(), enabled, refetchInterval: 60_000 })
}

export function useDiskTags(enabled = true) {
  return useQuery({ queryKey: ['disktags'], queryFn: () => tagsApi.disk(), enabled, refetchInterval: 60_000 })
}

export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => settingsApi.list(),
    ...usePoll(30_000, 60_000),
  })
}

export function useBackupVolumes() {
  return useQuery({
    queryKey: ['backupvolumes'],
    queryFn: () => backupVolumesApi.list(),
    ...usePoll(),
  })
}

export function useBackupTargets() {
  return useQuery({
    queryKey: ['backuptargets'],
    queryFn: () => backupTargetsApi.list(),
    ...usePoll(),
  })
}

export function useRecurringJobs() {
  return useQuery({
    queryKey: ['recurringjobs'],
    queryFn: () => recurringJobsApi.list(),
    ...usePoll(),
  })
}

export function useEngineImages(enabled = true) {
  return useQuery({
    queryKey: ['engineimages'],
    queryFn: () => engineImagesApi.list(),
    enabled,
    ...usePoll(),
  })
}

export function useBackingImages(enabled = true) {
  return useQuery({
    queryKey: ['backingimages'],
    queryFn: () => backingImagesApi.list(),
    enabled,
    ...usePoll(),
  })
}

export function useBackupBackingImages() {
  return useQuery({
    queryKey: ['backupbackingimages'],
    queryFn: () => backupBackingImagesApi.list(),
    ...usePoll(),
  })
}

export function useInstanceManagers() {
  return useQuery({
    queryKey: ['instancemanagers'],
    queryFn: () => instanceManagersApi.list(),
    ...usePoll(),
  })
}

export function useOrphans() {
  return useQuery({
    queryKey: ['orphans'],
    queryFn: () => orphansApi.list(),
    ...usePoll(),
  })
}

export function useSystemBackups() {
  return useQuery({
    queryKey: ['systembackups'],
    queryFn: () => systemBackupsApi.list(),
    ...usePoll(),
  })
}

export function useSystemRestores() {
  return useQuery({
    queryKey: ['systemrestores'],
    queryFn: () => systemRestoresApi.list(),
    ...usePoll(),
  })
}

export function useSupportBundles() {
  const connected = useSseConnected()
  return useQuery({
    queryKey: ['supportbundles'],
    queryFn: () => supportBundlesApi.list(),
    refetchInterval: (query) => {
      const active = query.state.data?.some((bundle) => {
        const state = (bundle.state ?? '').toLowerCase()
        return state !== '' && !['readyfordownload', 'error', 'failed'].includes(state)
      })
      if (active && !connected) return 3_000
      return connected ? 60_000 : 15_000
    },
  })
}

function useInvalidate() {
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vol: LonghornVolume) => volumesApi.remove(vol),
    ...optimisticRemove<LonghornVolume, LonghornVolume>(qc, 'volumes', (v) => v.name, ['volumes', 'dashboard'], {
      refetchOnSuccess: false,
    }),
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
      vol: LonghornVolume
      action: string
      params?: Record<string, unknown>
    }) => volumesApi.action(vol, action, params),
    onSuccess: () => inv(['volumes', 'dashboard']),
  })
}

export function useUpdateNode() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ node, body }: { node: Node; body: Record<string, unknown> }) =>
      nodesApi.update(node, body),
    // Patch ONLY the scheduling fields — never spread body.disks, which carries
    // stale server-computed capacity/conditions that would freeze wrong numbers.
    ...optimisticPatch<Node, { node: Node; body: Record<string, unknown> }>(
      qc,
      'nodes',
      (v) => v.node.name,
      (v) => pick(v.body, ['allowScheduling', 'evictionRequested', 'tags']) as Partial<Node>,
      ['nodes', 'dashboard'],
    ),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ setting, value }: { setting: Setting; value: string }) =>
      settingsApi.update(setting, value),
    ...optimisticPatch<Setting, { setting: Setting; value: string }>(
      qc,
      'settings',
      (v) => v.setting.name,
      (v) => ({ value: v.value }),
      ['settings'],
    ),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (bt: BackupTarget) => backupTargetsApi.remove(bt),
    ...optimisticRemove<BackupTarget, BackupTarget>(qc, 'backuptargets', (v) => v.name, ['backuptargets']),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ job, body }: { job: RecurringJob; body: Record<string, unknown> }) =>
      recurringJobsApi.update(job, body),
    ...optimisticPatch<RecurringJob, { job: RecurringJob; body: Record<string, unknown> }>(
      qc,
      'recurringjobs',
      (v) => v.job.name,
      (v) => v.body as Partial<RecurringJob>,
      ['recurringjobs'],
    ),
  })
}

export function useDeleteRecurringJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (job: RecurringJob) => recurringJobsApi.remove(job),
    ...optimisticRemove<RecurringJob, RecurringJob>(qc, 'recurringjobs', (v) => v.name, ['recurringjobs']),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (bv: BackupVolume) => backupVolumesApi.remove(bv),
    ...optimisticRemove<BackupVolume, BackupVolume>(qc, 'backupvolumes', (v) => v.name, ['backupvolumes']),
  })
}

export function useDeleteOrphan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (o: Orphan) => orphansApi.remove(o),
    ...optimisticRemove<Orphan, Orphan>(qc, 'orphans', (v) => v.name, ['orphans']),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (img: EngineImage) => engineImagesApi.remove(img),
    ...optimisticRemove<EngineImage, EngineImage>(qc, 'engineimages', (v) => v.name, ['engineimages']),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (img: BackingImage) => backingImagesApi.remove(img),
    ...optimisticRemove<BackingImage, BackingImage>(qc, 'backingimages', (v) => v.name, ['backingimages'], {
      refetchOnSuccess: false,
    }),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (bbi: BackupBackingImage) => backupBackingImagesApi.remove(bbi),
    ...optimisticRemove<BackupBackingImage, BackupBackingImage>(qc, 'backupbackingimages', (v) => v.name, ['backupbackingimages']),
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
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: SystemBackup) => systemBackupsApi.remove(b),
    ...optimisticRemove<SystemBackup, SystemBackup>(qc, 'systembackups', (v) => v.name, ['systembackups']),
  })
}

// --- Highland native ---

export function useAuditLog() {
  return useQuery({
    queryKey: ['audit'],
    queryFn: ({ signal }) => highlandGet<{ data: Array<Record<string, unknown>> }>('/audit', { signal }),
    refetchInterval: 15_000,
  })
}

export function useHighlandUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: ({ signal }) => highlandGet<{ data: Array<{ username: string; email?: string; role: string; disabled: boolean; mfaEnabled: boolean; mfaRequired: boolean; lastAuthenticatedAt?: string }> }>('/users', { signal }),
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
    queryFn: ({ signal }) => highlandGet<{ items: Array<{ severity: string; code: string; message: string }> }>('/health', { signal }),
    refetchInterval: 20_000,
  })
}

export function usePreflight() {
  return useQuery({
    queryKey: ['preflight'],
    queryFn: ({ signal }) => highlandGet<{ checks: Array<{ id: string; name: string; status: string; detail: string }> }>('/preflight', { signal }),
  })
}

export function useCapacity(enabled = true) {
  return useQuery({
    queryKey: ['capacity'],
    queryFn: ({ signal }) => highlandGet<{ usedBytes: number; totalBytes: number; note: string; seriesCount: number }>('/capacity', { signal }),
    enabled,
    refetchInterval: 15_000,
  })
}

export function useVolumeMetrics(name: string | undefined) {
  return useQuery({
    queryKey: ['volume-metrics', name],
    queryFn: ({ signal }) =>
      highlandGet<{
        series: Array<{ name: string; labels?: Record<string, string>; points: Array<{ t: string; v: number }> }>
        scrapeError?: string
      }>(`/volumes/${encodeURIComponent(name!)}/metrics`, { signal }),
    enabled: Boolean(name),
    refetchInterval: 10_000,
  })
}

export function useClusterMetrics() {
  return useQuery({
    queryKey: ['metrics'],
    queryFn: ({ signal }) =>
      highlandGet<{
        series: Array<{ name: string; labels?: Record<string, string>; points: Array<{ t: string; v: number }> }>
        scrapeError?: string
      }>('/metrics', { signal }),
    refetchInterval: 10_000,
  })
}

type BenchmarkPhase = 'Pending' | 'Running' | 'Succeeded' | 'Failed'
export type Benchmark = {
  name: string
  type: string
  nodeName?: string
  profile: string
  storageClass?: string
  size?: string
  pvcName?: string
  pvName?: string
  csiDriver?: string
  providerId?: string
  accessMode?: string
  volumeMode?: string
  topology?: Record<string, string>
  retainFailedPvc?: boolean
  phase: BenchmarkPhase
  message?: string
  createdAt: string
  completedAt?: string
  results?: Record<string, number>
  fioCmd?: string
  mode?: string
}
export type BenchmarkPage = {
  data: Benchmark[]
  page: { limit: number; continue?: string; total: number }
  meta: { observedAt: string; stale: boolean; partial: boolean; benchmarkMode: string }
}

export function useBenchmarks(cursor = '') {
  const sseConnected = useSseConnected()
  return useQuery({
    queryKey: ['benchmarks', cursor],
    queryFn: ({ signal }) => highlandGet<BenchmarkPage>(`/benchmarks?fields=summary&limit=50${cursor ? `&continue=${encodeURIComponent(cursor)}` : ''}`, { signal }),
    placeholderData: (previous) => previous,
    refetchInterval: (query) => {
      const benchmarks = Array.isArray(query.state.data?.data) ? query.state.data.data : []
      const active = benchmarks.some((benchmark) => benchmark.phase === 'Pending' || benchmark.phase === 'Running')
      if (active && !sseConnected) return 2_000
      return sseConnected ? 60_000 : 30_000
    },
  })
}

export function useBenchmark(name: string | null) {
  return useQuery({
    queryKey: ['benchmarks', 'detail', name],
    queryFn: ({ signal }) => highlandGet<Benchmark>(`/benchmarks/${encodeURIComponent(name!)}`, { signal }),
    enabled: Boolean(name),
    refetchInterval: (query) => {
      const phase = query.state.data?.phase
      return phase === 'Pending' || phase === 'Running' ? 2_000 : false
    },
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
    queryFn: ({ signal }) => highlandGet<Record<string, unknown>>('/compatibility', { signal }),
  })
}
