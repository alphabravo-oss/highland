import { lhDelete, lhGet, lhPost, lhPut, lhRequest } from './client'

export { lhPut, lhRequest }

/** Rancher-style Longhorn manager resource. */
export type LHResource = {
  id: string
  type: string
  name?: string
  links?: Record<string, string>
  actions?: Record<string, string>
  [key: string]: unknown
}

export type LHCollection<T extends LHResource = LHResource> = {
  type?: string
  resourceType?: string
  data: T[]
  links?: Record<string, string>
  actions?: Record<string, string>
}

export type Volume = LHResource & {
  name: string
  size?: string | number
  state?: string
  robustness?: string
  numberOfReplicas?: number
  dataEngine?: string
  frontend?: string
  created?: string
  kubernetesStatus?: {
    pvName?: string
    pvStatus?: string
    namespace?: string
    pvcName?: string
    workloadsStatus?: Array<{
      podName?: string
      podStatus?: string
      workloadName?: string
      workloadType?: string
    }>
  }
  replicas?: Array<{
    name?: string
    hostId?: string
    diskID?: string
    mode?: string
    running?: boolean
    failedAt?: string
  }>
  controllers?: Array<{
    name?: string
    hostId?: string
    endpoint?: string
    lastRestoredBackup?: string
  }>
  conditions?: Condition[] | Record<string, Condition>
  standby?: boolean
  restoreRequired?: boolean
  shareEndpoint?: string
  accessMode?: string
  dataLocality?: string
  snapshotCount?: number
  actualSize?: string | number
  snapshots?: unknown
}

export type Node = LHResource & {
  name: string
  address?: string
  allowScheduling?: boolean
  evictionRequested?: boolean
  conditions?: Condition[] | Record<string, Condition>
  disks?: Record<
    string,
    {
      path?: string
      allowScheduling?: boolean
      storageAvailable?: number
      storageMaximum?: number
      storageScheduled?: number
      storageReserved?: number
      diskType?: string
      // Block-device engine driver: "" (auto), "aio", "nvme". v2 data-engine only.
      diskDriver?: string
      tags?: string[]
      conditions?: Condition[] | Record<string, Condition>
    }
  >
  tags?: string[]
  region?: string
  zone?: string
}

export type Setting = LHResource & {
  name: string
  value?: string
  definition?: {
    displayName?: string
    description?: string
    category?: string
    type?: string
    required?: boolean
    readOnly?: boolean
    options?: string[]
    default?: string
  }
}

export type BackupVolume = LHResource & {
  name: string
  size?: string | number
  lastBackupName?: string
  lastBackupAt?: string
  created?: string
  backupTargetName?: string
  dataStored?: string | number
  messages?: Record<string, string>
}

export type BackupTarget = LHResource & {
  name: string
  backupTargetURL?: string
  credentialSecret?: string
  pollInterval?: string
  available?: boolean
  message?: string
}

export type RecurringJob = LHResource & {
  name: string
  task?: string
  cron?: string
  retain?: number
  concurrency?: number
  labels?: Record<string, string>
  groups?: string[]
  parameters?: Record<string, string>
}

export type EngineImage = LHResource & {
  name: string
  image?: string
  state?: string
  refCount?: number
  default?: boolean
}

export type BackingImage = LHResource & {
  name: string
  uuid?: string
  size?: number
  currentChecksum?: string
  minNumberOfCopies?: number
  diskFileStatusMap?: Record<string, { state?: string; message?: string }>
}

export type BackupBackingImage = LHResource & {
  name: string
  backingImageName?: string
  state?: string
  backupTargetName?: string
  size?: string | number
  url?: string
  secret?: string
  secretNamespace?: string
  created?: string
}

export type InstanceManager = LHResource & {
  name: string
  nodeID?: string
  image?: string
  currentState?: string
  instanceManagerType?: string
  dataEngine?: string
}

export type Orphan = LHResource & {
  name: string
  orphanType?: string
  nodeID?: string
  parameters?: Record<string, string>
}

export type SystemBackup = LHResource & {
  name: string
  version?: string
  state?: string
  error?: string
  created?: string
}

export type SystemRestore = LHResource & {
  name: string
  state?: string
  sourceSystemBackup?: string
  error?: string
}

export type SupportBundle = LHResource & {
  name?: string
  state?: string
  progress?: number
  errorMessage?: string
  imageURL?: string
}

export type Dashboard = {
  type?: string
  resourceType?: string
  // Longhorn dashboard shape varies; keep flexible
  nodes?: { total?: number; ready?: number }
  volumes?: { total?: number; healthy?: number; degraded?: number; faulted?: number; detached?: number }
  storage?: { total?: number; used?: number; available?: number }
  [key: string]: unknown
}

export type Event = LHResource & {
  eventType?: string
  reason?: string
  message?: string
  source?: string
  involvedObject?: { kind?: string; name?: string; namespace?: string }
  lastTimestamp?: string
  count?: number
}

// ---- collection helpers ----

export async function listCollection<T extends LHResource>(
  collection: string,
): Promise<T[]> {
  const path = collection.startsWith('/') ? collection : `/${collection}`
  const res = await lhGet<LHCollection<T>>(path)
  return Array.isArray(res?.data) ? res.data : []
}

export async function getResource<T extends LHResource>(
  collection: string,
  name: string,
): Promise<T> {
  return lhGet<T>(`/${collection}/${encodeURIComponent(name)}`)
}

export async function createResource<T extends LHResource>(
  collection: string,
  body: Record<string, unknown>,
): Promise<T> {
  return lhPost<T>(`/${collection}`, body)
}

export async function deleteResource(
  resource: LHResource,
): Promise<void> {
  const self = resource.links?.self
  if (self) {
    await lhRequest(self, 'DELETE')
    return
  }
  if (resource.type && resource.id) {
    await lhDelete(`/${resource.type}s/${encodeURIComponent(resource.id)}`)
    return
  }
  throw new Error('resource has no self link or id')
}

export async function updateResource<T extends LHResource>(
  resource: LHResource,
  body: Record<string, unknown>,
): Promise<T> {
  const self = resource.links?.self
  if (!self) throw new Error('resource has no self link')
  return lhRequest<T>(self, 'PUT', body)
}

/**
 * Execute a manager action by name using the resource's actions map (stock UI pattern).
 * Falls back to `?action=name` on self link only if action URL missing.
 */
export async function execAction<T = unknown>(
  resource: LHResource,
  actionName: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const actionUrl = resource.actions?.[actionName]
  if (actionUrl) {
    return lhRequest<T>(actionUrl, 'POST', params)
  }
  const self = resource.links?.self
  if (self) {
    const sep = self.includes('?') ? '&' : '?'
    return lhRequest<T>(`${self}${sep}action=${encodeURIComponent(actionName)}`, 'POST', params)
  }
  throw new Error(`action "${actionName}" not available on resource ${resource.id ?? resource.name}`)
}

export function hasAction(resource: LHResource | null | undefined, name: string): boolean {
  return Boolean(resource?.actions?.[name])
}

// ---- domain APIs ----

export const volumesApi = {
  list: () => listCollection<Volume>('volumes'),
  get: (name: string) => getResource<Volume>('volumes', name),
  create: (body: Record<string, unknown>) => createResource<Volume>('volumes', body),
  remove: (vol: Volume) => deleteResource(vol),
  action: (vol: Volume, name: string, params?: Record<string, unknown>) =>
    execAction(vol, name, params),
}

export const nodesApi = {
  list: () => listCollection<Node>('nodes'),
  get: (name: string) => getResource<Node>('nodes', name),
  update: (node: Node, body: Record<string, unknown>) => updateResource<Node>(node, body),
  action: (node: Node, name: string, params?: Record<string, unknown>) =>
    execAction(node, name, params),
}

/** Existing node/disk tags for autocomplete in scheduling/tag forms. */
export const tagsApi = {
  node: () => fetchTags('/nodetags'),
  disk: () => fetchTags('/disktags'),
}

async function fetchTags(path: string): Promise<string[]> {
  const res = await lhGet<{ data?: Array<string | { name?: string; tag?: string }> }>(path)
  return (res?.data ?? [])
    .map((t) => (typeof t === 'string' ? t : (t.name ?? t.tag ?? '')))
    .filter(Boolean)
}

export const settingsApi = {
  list: () => listCollection<Setting>('settings'),
  get: (name: string) => getResource<Setting>('settings', name),
  update: (setting: Setting, value: string) =>
    updateResource<Setting>(setting, { ...setting, value }),
}

export const backupVolumesApi = {
  list: () => listCollection<BackupVolume>('backupvolumes'),
  get: (name: string) => getResource<BackupVolume>('backupvolumes', name),
  remove: (bv: BackupVolume) => deleteResource(bv),
  action: (bv: BackupVolume, name: string, params?: Record<string, unknown>) =>
    execAction(bv, name, params),
}

export const backupTargetsApi = {
  list: () => listCollection<BackupTarget>('backuptargets'),
  create: (body: Record<string, unknown>) => createResource<BackupTarget>('backuptargets', body),
  remove: (bt: BackupTarget) => deleteResource(bt),
  action: (bt: BackupTarget, name: string, params?: Record<string, unknown>) =>
    execAction(bt, name, params),
}

export const recurringJobsApi = {
  list: () => listCollection<RecurringJob>('recurringjobs'),
  create: (body: Record<string, unknown>) => createResource<RecurringJob>('recurringjobs', body),
  update: (job: RecurringJob, body: Record<string, unknown>) =>
    updateResource<RecurringJob>(job, body),
  remove: (job: RecurringJob) => deleteResource(job),
}

export const engineImagesApi = {
  list: () => listCollection<EngineImage>('engineimages'),
  create: (body: Record<string, unknown>) => createResource<EngineImage>('engineimages', body),
  remove: (img: EngineImage) => deleteResource(img),
}

export const backingImagesApi = {
  list: () => listCollection<BackingImage>('backingimages'),
  create: (body: Record<string, unknown>) => createResource<BackingImage>('backingimages', body),
  remove: (img: BackingImage) => deleteResource(img),
  action: (img: BackingImage, name: string, params?: Record<string, unknown>) =>
    execAction(img, name, params),
}

export const backupBackingImagesApi = {
  list: () => listCollection<BackupBackingImage>('backupbackingimages'),
  remove: (bbi: BackupBackingImage) => deleteResource(bbi),
  action: (bbi: BackupBackingImage, name: string, params?: Record<string, unknown>) =>
    execAction(bbi, name, params),
}

export const instanceManagersApi = {
  list: () => listCollection<InstanceManager>('instancemanagers'),
}

export const orphansApi = {
  list: () => listCollection<Orphan>('orphans'),
  remove: (o: Orphan) => deleteResource(o),
}

export const systemBackupsApi = {
  list: () => listCollection<SystemBackup>('systembackups'),
  create: (body: Record<string, unknown>) => createResource<SystemBackup>('systembackups', body),
  remove: (b: SystemBackup) => deleteResource(b),
}

export const systemRestoresApi = {
  list: () => listCollection<SystemRestore>('systemrestores'),
  create: (body: Record<string, unknown>) => createResource<SystemRestore>('systemrestores', body),
  remove: (r: SystemRestore) => deleteResource(r),
}

export const supportBundlesApi = {
  list: () => listCollection<SupportBundle>('supportbundles'),
  create: (body: Record<string, unknown> = {}) =>
    createResource<SupportBundle>('supportbundles', body),
  get: (name: string) => getResource<SupportBundle>('supportbundles', name),
}

export const dashboardApi = {
  get: () => lhGet<Dashboard>('/dashboard'),
}

export const eventsApi = {
  list: async () => {
    try {
      return await listCollection<Event>('events')
    } catch {
      // Some versions use different path; return empty rather than hard-fail dashboard
      return [] as Event[]
    }
  },
}

export const volumeAttachmentsApi = {
  list: () => listCollection<LHResource>('volumeattachments'),
  get: (id: string) => getResource<LHResource>('volumeattachments', id),
}

export function formatBytes(value: string | number | undefined | null): string {
  if (value === undefined || value === null || value === '') return '—'
  const n = typeof value === 'string' ? Number(value) : value
  if (!Number.isFinite(n) || n < 0) return String(value)
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function parseSizeToBytes(input: string): string {
  const trimmed = input.trim()
  const m =
    /^(\d+(?:\.\d+)?)\s*(kib|mib|gib|tib|ki|mi|gi|ti|kb|mb|gb|tb|b)?$/i.exec(
      trimmed,
    )
  if (!m) {
    // raw number as bytes string
    if (/^\d+$/.test(trimmed)) return trimmed
    throw new Error('invalid size; use e.g. 10Gi or bytes')
  }
  const num = parseFloat(m[1]!)
  let unit = (m[2] ?? 'b').toLowerCase()
  // Normalize k8s-style Gi/Mi/Ki (without trailing b) to gib/mib/kib
  if (unit === 'ki' || unit === 'k') unit = 'kib'
  if (unit === 'mi' || unit === 'm') unit = 'mib'
  if (unit === 'gi' || unit === 'g') unit = 'gib'
  if (unit === 'ti' || unit === 't') unit = 'tib'
  const mult: Record<string, number> = {
    b: 1,
    kib: 1024,
    mib: 1024 ** 2,
    gib: 1024 ** 3,
    tib: 1024 ** 4,
    kb: 1000,
    mb: 1000 ** 2,
    gb: 1000 ** 3,
    tb: 1000 ** 4,
  }
  return String(Math.round(num * (mult[unit] ?? 1)))
}

/** A Longhorn condition (volume/node/disk). */
export type Condition = { type?: string; status?: string; message?: string; reason?: string }

/**
 * Longhorn returns `conditions` as a type-keyed map (real manager) or, in some
 * fixtures/older shapes, an array. Normalize to an array so callers can filter.
 */
export function toConditionArray(
  c?: Condition[] | Record<string, Condition> | null,
): Condition[] {
  if (!c) return []
  if (Array.isArray(c)) return c
  return Object.entries(c).map(([type, v]) => ({ type, ...(v ?? {}) }))
}
