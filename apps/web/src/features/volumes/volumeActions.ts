/**
 * Stock Longhorn VolumeActions.js keys — enable from resource.actions map only.
 * Labels are English fallbacks; UI should prefer t(`volumeActions.${key}`).
 * @see HIGHLAND_PLAN.md §8.4
 */
export const VOLUME_ACTION_DEFS = [
  { key: 'attach', label: 'Attach', priority: 'P0', needsHost: true },
  { key: 'detach', label: 'Detach', priority: 'P0' },
  { key: 'salvage', label: 'Salvage', priority: 'P0', needsReplicas: true },
  { key: 'engineUpgrade', label: 'Upgrade engine', priority: 'P1', needsImage: true },
  { key: 'updateReplicaCount', label: 'Replica count', priority: 'P0', field: 'replicaCount', type: 'number' },
  { key: 'updateDataLocality', label: 'Data locality', priority: 'P1', field: 'dataLocality', options: ['disabled', 'best-effort', 'strict-local'] },
  { key: 'updateSnapshotDataIntegrity', label: 'Snapshot integrity', priority: 'P1', field: 'snapshotDataIntegrity', options: ['ignored', 'disabled', 'enabled', 'fast-check'] },
  { key: 'updateAccessMode', label: 'Access mode', priority: 'P1', field: 'accessMode', options: ['rwo', 'rwx'] },
  { key: 'updateBackupTargetName', label: 'Backup target', priority: 'P1', field: 'backupTargetName', type: 'text' },
  { key: 'updateReplicaAutoBalance', label: 'Replica auto-balance', priority: 'P1', field: 'replicaAutoBalance', options: ['ignored', 'disabled', 'least-effort', 'best-effort'] },
  { key: 'updateSnapshotMaxCount', label: 'Snapshot max count', priority: 'P1', field: 'snapshotMaxCount', type: 'number' },
  { key: 'updateSnapshotMaxSize', label: 'Snapshot max size', priority: 'P1', field: 'snapshotMaxSize', type: 'text' },
  { key: 'offlineReplicaRebuilding', label: 'Offline rebuild', priority: 'P1', field: 'offlineReplicaRebuilding', options: ['ignored', 'disabled', 'enabled'] },
  { key: 'cloneVolume', label: 'Clone', priority: 'P0', needsClone: true },
  { key: 'expand', label: 'Expand', priority: 'P0', needsSize: true },
  { key: 'cancelExpansion', label: 'Cancel expansion', priority: 'P0' },
  { key: 'pvCreate', label: 'Create PV', priority: 'P0', needsPv: true },
  { key: 'pvcCreate', label: 'Create PVC', priority: 'P0', needsPvc: true },
  { key: 'activate', label: 'Activate DR', priority: 'P1', field: 'frontend', options: ['blockdev', 'iscsi', 'nvmf', 'ublk'] },
  { key: 'trimFilesystem', label: 'Trim filesystem', priority: 'P1' },
  { key: 'snapshotPurge', label: 'Purge snapshots', priority: 'P1' },
  { key: 'snapshotCreate', label: 'Snapshot', priority: 'P0' },
  { key: 'recurringJobAdd', label: 'Add recurring job', priority: 'P0' },
  { key: 'recurringJobDelete', label: 'Remove recurring job', priority: 'P0' },
  { key: 'updateUnmapMarkSnapChainRemoved', label: 'Unmap snap chain', priority: 'P2', field: 'unmapMarkSnapChainRemoved', options: ['ignored', 'disabled', 'enabled'] },
  { key: 'updateReplicaSoftAntiAffinity', label: 'Replica soft AA', priority: 'P2', field: 'replicaSoftAntiAffinity', options: ['ignored', 'enabled', 'disabled'] },
  { key: 'updateReplicaZoneSoftAntiAffinity', label: 'Zone soft AA', priority: 'P2', field: 'replicaZoneSoftAntiAffinity', options: ['ignored', 'enabled', 'disabled'] },
  { key: 'updateReplicaDiskSoftAntiAffinity', label: 'Disk soft AA', priority: 'P2', field: 'replicaDiskSoftAntiAffinity', options: ['ignored', 'enabled', 'disabled'] },
  { key: 'updateFreezeFilesystemForSnapshot', label: 'Freeze FS for snap', priority: 'P2', field: 'freezeFilesystemForSnapshot', options: ['ignored', 'enabled', 'disabled'] },
  { key: 'updateReplicaRebuildingBandwidthLimit', label: 'Rebuild bandwidth limit', priority: 'P2', field: 'replicaRebuildingBandwidthLimit', type: 'number' },
  { key: 'updateUblkNumberOfQueue', label: 'UBLK queues', priority: 'P2', field: 'ublkNumberOfQueue', type: 'number' },
  { key: 'updateUblkQueueDepth', label: 'UBLK queue depth', priority: 'P2', field: 'ublkQueueDepth', type: 'number' },
  { key: 'updateRebuildConcurrentSyncLimit', label: 'Rebuild concurrent sync limit', priority: 'P2', field: 'rebuildConcurrentSyncLimit', type: 'number' },
] as const

export type VolumeActionDef = (typeof VOLUME_ACTION_DEFS)[number]

/**
 * Ordered grouping for the volume Actions menu so a long action list reads as
 * labeled sections (Lifecycle, Snapshots, …) instead of one flat scroll.
 * Any action key not listed here falls into the trailing "advanced" bucket.
 */
const ACTION_GROUPS = [
  {
    id: 'lifecycle',
    keys: ['attach', 'detach', 'salvage', 'activate', 'expand', 'cancelExpansion', 'cloneVolume', 'trimFilesystem', 'engineUpgrade'],
  },
  {
    id: 'snapshots',
    keys: ['snapshotCreate', 'snapshotPurge', 'updateSnapshotDataIntegrity', 'updateSnapshotMaxCount', 'updateSnapshotMaxSize', 'updateFreezeFilesystemForSnapshot', 'updateUnmapMarkSnapChainRemoved'],
  },
  {
    id: 'replicas',
    keys: ['updateReplicaCount', 'updateDataLocality', 'updateReplicaAutoBalance', 'offlineReplicaRebuilding', 'updateReplicaSoftAntiAffinity', 'updateReplicaZoneSoftAntiAffinity', 'updateReplicaDiskSoftAntiAffinity', 'updateReplicaRebuildingBandwidthLimit', 'updateRebuildConcurrentSyncLimit'],
  },
  { id: 'access', keys: ['updateAccessMode', 'updateBackupTargetName'] },
  { id: 'kubernetes', keys: ['pvCreate', 'pvcCreate', 'recurringJobAdd', 'recurringJobDelete'] },
] as const

/**
 * Split available action defs into ordered { id, items } groups for rendering.
 * Unlisted keys collect under a trailing `advanced` group so nothing is dropped.
 */
export function groupActions<T extends { key: string }>(defs: T[]): Array<{ id: string; items: T[] }> {
  const seen = new Set<string>()
  const groups: Array<{ id: string; items: T[] }> = ACTION_GROUPS.map((g) => {
    const items = defs.filter((d) => (g.keys as readonly string[]).includes(d.key))
    items.forEach((d) => seen.add(d.key))
    return { id: g.id, items }
  }).filter((g) => g.items.length > 0)
  const rest = defs.filter((d) => !seen.has(d.key))
  if (rest.length) groups.push({ id: 'advanced', items: rest })
  return groups
}

export const BULK_ACTIONS = [
  { key: 'detach', label: 'Bulk detach', labelKey: 'volumeActions.bulkDetach' },
  { key: 'snapshotCreate', label: 'Bulk snapshot', labelKey: 'volumeActions.bulkSnapshot' },
  { key: 'snapshotPurge', label: 'Bulk purge snapshots', labelKey: 'volumeActions.bulkSnapshotPurge' },
  { key: 'trimFilesystem', label: 'Bulk trim filesystem', labelKey: 'volumeActions.bulkTrimFilesystem' },
  { key: 'updateReplicaCount', label: 'Bulk replica count', needsValue: true, labelKey: 'volumeActions.bulkReplicaCount' },
  { key: 'updateDataLocality', label: 'Bulk data locality', needsValue: true, labelKey: 'volumeActions.bulkDataLocality' },
  { key: 'updateAccessMode', label: 'Bulk access mode', needsValue: true, labelKey: 'volumeActions.bulkAccessMode' },
  { key: 'updateSnapshotDataIntegrity', label: 'Bulk snapshot integrity', needsValue: true, labelKey: 'volumeActions.bulkSnapshotDataIntegrity' },
  { key: 'updateReplicaAutoBalance', label: 'Bulk replica auto-balance', needsValue: true, labelKey: 'volumeActions.bulkReplicaAutoBalance' },
  { key: 'updateBackupTargetName', label: 'Bulk backup target', needsValue: true, labelKey: 'volumeActions.bulkBackupTargetName' },
  { key: 'offlineReplicaRebuilding', label: 'Bulk offline rebuild', needsValue: true, labelKey: 'volumeActions.bulkOfflineReplicaRebuilding' },
  { key: 'engineUpgrade', label: 'Bulk engine upgrade', needsValue: true, labelKey: 'volumeActions.bulkEngineUpgrade' },
  { key: 'activate', label: 'Bulk activate DR', labelKey: 'volumeActions.bulkActivate' },
  { key: 'delete', label: 'Bulk delete', destructive: true, labelKey: 'volumeActions.bulkDelete' },
] as const

/** Translate a volume action key with English fallback. */
export function volumeActionLabel(
  t: (key: string, opts?: { defaultValue?: string }) => string,
  key: string,
  fallback?: string,
): string {
  return t(`volumeActions.${key}`, { defaultValue: fallback ?? key })
}
