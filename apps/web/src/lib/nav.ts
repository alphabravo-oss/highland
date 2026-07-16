import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  Archive,
  Box,
  Boxes,
  CalendarClock,
  Camera,
  Cable,
  CloudUpload,
  Cpu,
  Database,
  DatabaseBackup,
  Gauge,
  GitBranch,
  Ghost,
  HardDrive,
  Image,
  Info,
  Layers3,
  LayoutDashboard,
  LifeBuoy,
  ListTree,
  Network,
  Server,
  Settings,
  Shield,
  Trash2,
  Workflow,
} from 'lucide-react'
import type { ProviderDescriptor } from '@/api/storage/types'

export type NavItem = {
  id: string
  labelKey: string
  /** English fallback prevents raw translation keys during additive releases. */
  label: string
  path: string
  icon: LucideIcon
  roles?: Array<'admin' | 'operator' | 'viewer'>
  /** Exact links do not remain active for provider child routes. */
  end?: boolean
}

export type NavGroup = {
  id: string
  labelKey: string
  label: string
  items: NavItem[]
  roles?: Array<'admin' | 'operator' | 'viewer'>
}

export type WorkspaceProvider = Pick<
  ProviderDescriptor,
  'id' | 'kind' | 'displayName' | 'supportLevel' | 'capabilities' | 'health' | 'metadata'
>

const allStorageGroups: NavGroup[] = [
  {
    id: 'all-storage',
    labelKey: 'nav.allStorage',
    label: 'All storage',
    items: [
      { id: 'storage-providers', labelKey: 'nav.providers', label: 'Providers', path: '/storage/providers', icon: ListTree, end: true },
      { id: 'storage-inventory', labelKey: 'nav.inventory', label: 'Inventory', path: '/storage/inventory', icon: Database },
      { id: 'storage-operations', labelKey: 'nav.storageOperations', label: 'Operations', path: '/storage/operations', icon: Workflow },
      { id: 'storage-events', labelKey: 'nav.storageEvents', label: 'Events', path: '/storage/events', icon: Activity },
      { id: 'storage-insights', labelKey: 'nav.storageInsights', label: 'Context & insights', path: '/storage/insights', icon: GitBranch },
      { id: 'benchmarks', labelKey: 'nav.benchmarks', label: 'Benchmarks', path: '/benchmarks', icon: Gauge },
    ],
  },
]

const systemGroups: NavGroup[] = [
  {
    id: 'system',
    labelKey: 'nav.system',
    label: 'System',
    items: [
      { id: 'status', labelKey: 'nav.status', label: 'Status', path: '/status', icon: Info },
      { id: 'admin', labelKey: 'nav.administration', label: 'Administration', path: '/admin', icon: Shield, roles: ['admin'], end: true },
    ],
  },
]

/** Admin destinations are shown on the Administration page, not in the sidebar. */
const adminPageGroups: NavGroup[] = [
  {
    id: 'admin-pages',
    labelKey: 'nav.administration',
    label: 'Administration',
    roles: ['admin'],
    items: [
      { id: 'admin-users', labelKey: 'nav.users', label: 'Users', path: '/admin/users', icon: Shield, roles: ['admin'], end: true },
      { id: 'sso', labelKey: 'nav.sso', label: 'Enterprise SSO', path: '/admin/sso', icon: Shield, roles: ['admin'], end: true },
      { id: 'audit', labelKey: 'nav.auditLog', label: 'Audit Log', path: '/admin/audit', icon: Shield, roles: ['admin'], end: true },
    ],
  },
]

const longhornGroups: NavGroup[] = [
  {
    id: 'longhorn-overview',
    labelKey: 'nav.longhorn',
    label: 'Longhorn',
    items: [
      { id: 'longhorn-dashboard', labelKey: 'nav.dashboard', label: 'Dashboard', path: '/dashboard', icon: LayoutDashboard },
      { id: 'longhorn-provider', labelKey: 'nav.providerOverview', label: 'Provider overview', path: '/storage/providers/longhorn', icon: ListTree, end: true },
      { id: 'longhorn-context', labelKey: 'nav.storageInsights', label: 'Context & insights', path: '/storage/providers/longhorn/context', icon: GitBranch, end: true },
      { id: 'longhorn-operations', labelKey: 'nav.storageOperations', label: 'Operations', path: '/storage/operations?provider=longhorn', icon: Workflow },
    ],
  },
  {
    id: 'longhorn-storage',
    labelKey: 'nav.storage',
    label: 'Storage',
    items: [
      { id: 'volumes', labelKey: 'nav.volumes', label: 'Volumes', path: '/volumes', icon: HardDrive },
      { id: 'nodes', labelKey: 'nav.nodes', label: 'Nodes & Disks', path: '/nodes', icon: Server },
    ],
  },
  {
    id: 'longhorn-data-protection',
    labelKey: 'nav.dataProtection',
    label: 'Data Protection',
    items: [
      { id: 'backups', labelKey: 'nav.backups', label: 'Backups', path: '/backups', icon: Archive },
      { id: 'backup-targets', labelKey: 'nav.backupTargets', label: 'Backup Targets', path: '/backup-targets', icon: CloudUpload },
      { id: 'recurring-jobs', labelKey: 'nav.recurringJobs', label: 'Recurring Jobs', path: '/recurring-jobs', icon: CalendarClock },
      { id: 'system-backups', labelKey: 'nav.systemBackups', label: 'System Backups', path: '/system-backups', icon: DatabaseBackup },
    ],
  },
  {
    id: 'longhorn-performance',
    labelKey: 'nav.performance',
    label: 'Performance',
    items: [
      { id: 'live-io', labelKey: 'nav.liveIo', label: 'Live I/O', path: '/performance', icon: Activity },
      { id: 'longhorn-benchmarks', labelKey: 'nav.benchmarks', label: 'Benchmarks', path: '/benchmarks?provider=longhorn', icon: Gauge },
    ],
  },
  {
    id: 'longhorn-runtime',
    labelKey: 'nav.runtime',
    label: 'Images & Runtime',
    items: [
      { id: 'backing-images', labelKey: 'nav.backingImages', label: 'Backing Images', path: '/backing-images', icon: Image },
      { id: 'engine-images', labelKey: 'nav.engineImages', label: 'Engine Images', path: '/engine-images', icon: Box },
      { id: 'instance-managers', labelKey: 'nav.instanceManagers', label: 'Instance Managers', path: '/instance-managers', icon: Cpu },
    ],
  },
  {
    id: 'longhorn-maintenance',
    labelKey: 'nav.maintenance',
    label: 'Maintenance',
    items: [
      { id: 'orphans', labelKey: 'nav.orphans', label: 'Orphaned Data', path: '/orphans', icon: Ghost },
      { id: 'support-bundle', labelKey: 'nav.supportBundle', label: 'Support Bundle', path: '/support-bundle', icon: LifeBuoy },
      { id: 'preflight', labelKey: 'nav.preflight', label: 'Preflight', path: '/preflight', icon: Trash2 },
      { id: 'settings', labelKey: 'nav.settings', label: 'Settings', path: '/settings', icon: Settings },
    ],
  },
]

function providerFilter(path: string, providerId: string) {
  const separator = path.includes('?') ? '&' : '?'
  return `${path}${separator}provider=${encodeURIComponent(providerId)}`
}

function commonInventoryItems(provider: WorkspaceProvider): NavItem[] {
  const has = (capability: string) => provider.capabilities.includes(capability)
  const items: NavItem[] = [
    { id: `${provider.id}-classes`, labelKey: 'nav.storageClasses', label: 'Storage Classes', path: providerFilter('/storage/classes', provider.id), icon: Layers3 },
  ]
  if (has('inventory.claims.read')) {
    items.push({ id: `${provider.id}-claims`, labelKey: 'nav.claims', label: 'Claims & Workloads', path: providerFilter('/storage/claims', provider.id), icon: Database })
  }
  if (has('inventory.volumes.read')) {
    items.push({ id: `${provider.id}-volumes`, labelKey: 'nav.persistentVolumes', label: 'Persistent Volumes', path: providerFilter('/storage/volumes', provider.id), icon: HardDrive })
  }
  if (has('inventory.snapshots.read')) {
    items.push({ id: `${provider.id}-snapshots`, labelKey: 'nav.snapshots', label: 'Snapshots', path: providerFilter('/storage/snapshots', provider.id), icon: Camera })
  }
  if (has('inventory.attachments.read')) {
    items.push({ id: `${provider.id}-attachments`, labelKey: 'nav.attachments', label: 'Attachments', path: providerFilter('/storage/attachments', provider.id), icon: Cable })
  }
  if (has('inventory.capacity.read')) {
    items.push({ id: `${provider.id}-capacity`, labelKey: 'nav.capacity', label: 'Capacity', path: providerFilter('/storage/capacity', provider.id), icon: Gauge })
  }
  if (has('inventory.events.read')) {
    items.push({ id: `${provider.id}-events`, labelKey: 'nav.storageEvents', label: 'Events', path: providerFilter('/storage/events', provider.id), icon: Activity })
  }
  return items
}

function rookCephGroups(provider: WorkspaceProvider): NavGroup[] {
  const providerPath = `/storage/providers/${encodeURIComponent(provider.id)}`
  const dashboardConfigured = provider.metadata?.dashboard === 'configured'
  const cephItems: NavItem[] = [
    { id: `${provider.id}-pools`, labelKey: 'nav.pools', label: 'Block Pools', path: `${providerPath}/ceph/pools`, icon: Layers3 },
    { id: `${provider.id}-filesystems`, labelKey: 'nav.filesystems', label: 'CephFS Filesystems', path: `${providerPath}/ceph/filesystems`, icon: Database },
    { id: `${provider.id}-mirroring`, labelKey: 'nav.mirroring', label: 'RBD Mirroring', path: `${providerPath}/ceph/mirroring`, icon: Network },
  ]
  if (dashboardConfigured) {
    cephItems.unshift(
      { id: `${provider.id}-quorum`, labelKey: 'nav.quorum', label: 'MON Quorum', path: `${providerPath}/ceph/quorum`, icon: Shield },
      { id: `${provider.id}-osds`, labelKey: 'nav.osds', label: 'OSDs', path: `${providerPath}/ceph/osds`, icon: Server },
    )
    cephItems.push({ id: `${provider.id}-rbd-images`, labelKey: 'nav.rbdImages', label: 'RBD Images', path: `${providerPath}/ceph/rbd-images`, icon: HardDrive })
  }
  return [
    {
      id: `${provider.id}-overview`,
      labelKey: 'nav.rookCeph',
      label: 'Rook / Ceph',
      items: [
        { id: `${provider.id}-provider-overview`, labelKey: 'nav.clusterHealth', label: 'Cluster Health', path: providerPath, icon: LayoutDashboard, end: true },
        { id: `${provider.id}-context`, labelKey: 'nav.storageInsights', label: 'Context & insights', path: `${providerPath}/context`, icon: GitBranch, end: true },
        { id: `${provider.id}-operations`, labelKey: 'nav.storageOperations', label: 'Operations', path: providerFilter('/storage/operations', provider.id), icon: Workflow },
      ],
    },
    { id: `${provider.id}-ceph`, labelKey: 'nav.cephResources', label: 'Ceph Resources', items: cephItems },
    { id: `${provider.id}-inventory`, labelKey: 'nav.kubernetesInventory', label: 'Kubernetes Inventory', items: commonInventoryItems(provider) },
  ]
}

function openEBSGroups(provider: WorkspaceProvider): NavGroup[] {
  const providerPath = `/storage/providers/${encodeURIComponent(provider.id)}`
  const installed = (engine: string) => provider.metadata?.[`engine.${engine}`] === 'true'
  const groups: NavGroup[] = [
    {
      id: `${provider.id}-overview`,
      labelKey: 'nav.openebs',
      label: 'OpenEBS',
      items: [
        { id: `${provider.id}-provider-overview`, labelKey: 'nav.providerOverview', label: 'Overview', path: providerPath, icon: LayoutDashboard, end: true },
        { id: `${provider.id}-components`, labelKey: 'nav.components', label: 'Components', path: `${providerPath}/openebs/components`, icon: Boxes },
        { id: `${provider.id}-context`, labelKey: 'nav.storageInsights', label: 'Context & insights', path: `${providerPath}/context`, icon: GitBranch, end: true },
        { id: `${provider.id}-operations`, labelKey: 'nav.storageOperations', label: 'Operations', path: providerFilter('/storage/operations', provider.id), icon: Workflow },
      ],
    },
  ]
  if (installed('mayastor')) {
    groups.push({
      id: `${provider.id}-mayastor`,
      labelKey: 'nav.mayastor',
      label: 'Replicated PV / Mayastor',
      items: [
        { id: `${provider.id}-disk-pools`, labelKey: 'nav.diskPools', label: 'Disk Pools', path: `${providerPath}/openebs/disk-pools`, icon: Database },
      ],
    })
  }
  if (installed('lvm')) {
    groups.push({
      id: `${provider.id}-lvm`,
      labelKey: 'nav.localPvLvm',
      label: 'LocalPV LVM',
      items: [
        { id: `${provider.id}-lvm-nodes`, labelKey: 'nav.nodes', label: 'Nodes & Volume Groups', path: `${providerPath}/openebs/lvm-nodes`, icon: Server },
        { id: `${provider.id}-lvm-volumes`, labelKey: 'nav.volumes', label: 'Volumes', path: `${providerPath}/openebs/lvm-volumes`, icon: HardDrive },
        { id: `${provider.id}-lvm-snapshots`, labelKey: 'nav.snapshots', label: 'Snapshots', path: `${providerPath}/openebs/lvm-snapshots`, icon: Camera },
      ],
    })
  }
  if (installed('zfs')) {
    groups.push({
      id: `${provider.id}-zfs`,
      labelKey: 'nav.localPvZfs',
      label: 'LocalPV ZFS',
      items: [
        { id: `${provider.id}-zfs-nodes`, labelKey: 'nav.nodes', label: 'Nodes & Pools', path: `${providerPath}/openebs/zfs-nodes`, icon: Server },
        { id: `${provider.id}-zfs-volumes`, labelKey: 'nav.volumes', label: 'Volumes', path: `${providerPath}/openebs/zfs-volumes`, icon: HardDrive },
        { id: `${provider.id}-zfs-snapshots`, labelKey: 'nav.snapshots', label: 'Snapshots', path: `${providerPath}/openebs/zfs-snapshots`, icon: Camera },
        { id: `${provider.id}-zfs-backups`, labelKey: 'nav.backups', label: 'Backups', path: `${providerPath}/openebs/zfs-backups`, icon: Archive },
        { id: `${provider.id}-zfs-restores`, labelKey: 'nav.restores', label: 'Restores', path: `${providerPath}/openebs/zfs-restores`, icon: DatabaseBackup },
      ],
    })
  }
  if (installed('hostpath')) {
    groups.push({
      id: `${provider.id}-hostpath`,
      labelKey: 'nav.hostpath',
      label: 'HostPath',
      items: [
        { id: `${provider.id}-hostpath-volumes`, labelKey: 'nav.localVolumes', label: 'Local Volumes', path: `${providerPath}/openebs/hostpath-volumes`, icon: HardDrive },
      ],
    })
  }
  groups.push({ id: `${provider.id}-inventory`, labelKey: 'nav.kubernetesInventory', label: 'Kubernetes Inventory', items: commonInventoryItems(provider) })
  return groups
}

function genericProviderGroups(provider: WorkspaceProvider): NavGroup[] {
  const providerPath = `/storage/providers/${encodeURIComponent(provider.id)}`
  return [
    {
      id: `${provider.id}-overview`,
      labelKey: 'nav.csiProvider',
      label: 'CSI Provider',
      items: [
        { id: `${provider.id}-provider-overview`, labelKey: 'nav.providerOverview', label: 'Provider overview', path: providerPath, icon: LayoutDashboard, end: true },
        { id: `${provider.id}-context`, labelKey: 'nav.storageInsights', label: 'Context & insights', path: `${providerPath}/context`, icon: GitBranch, end: true },
        { id: `${provider.id}-operations`, labelKey: 'nav.storageOperations', label: 'Operations', path: providerFilter('/storage/operations', provider.id), icon: Workflow },
      ],
    },
    { id: `${provider.id}-inventory`, labelKey: 'nav.kubernetesInventory', label: 'Kubernetes Inventory', items: commonInventoryItems(provider) },
  ]
}

export function filterNavForRole(groups: NavGroup[], role: string | undefined): NavGroup[] {
  const normalizedRole = (role ?? 'viewer') as 'admin' | 'operator' | 'viewer'
  return groups
    .filter((group) => !group.roles || group.roles.includes(normalizedRole))
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !item.roles || item.roles.includes(normalizedRole)),
    }))
    .filter((group) => group.items.length > 0)
}

export function navigationForWorkspace(provider: WorkspaceProvider | undefined, role: string | undefined): NavGroup[] {
  let workspaceGroups: NavGroup[]
  if (!provider) workspaceGroups = allStorageGroups
  else if (provider.kind === 'longhorn' || provider.id === 'longhorn') workspaceGroups = longhornGroups
  else if (provider.kind === 'rook-ceph') workspaceGroups = rookCephGroups(provider)
  else if (provider.kind === 'openebs') workspaceGroups = openEBSGroups(provider)
  else workspaceGroups = genericProviderGroups(provider)
  return filterNavForRole([...workspaceGroups, ...systemGroups], role)
}

const legacyLonghornPaths = new Set(
  longhornGroups
    .flatMap((group) => group.items.map((item) => item.path.split('?')[0]))
    .filter((path) => path !== '/storage/operations' && path !== '/benchmarks'),
)

function candidateProviderId(pathname: string, search: string): string | undefined {
  const providerMatch = pathname.match(/^\/storage\/providers\/([^/]+)(?:\/|$)/)
  if (providerMatch) {
    const encodedProvider = providerMatch[1]
    if (!encodedProvider) return undefined
    try {
      return decodeURIComponent(encodedProvider)
    } catch {
      return encodedProvider
    }
  }
  const queryProvider = new URLSearchParams(search).get('provider')
  if (queryProvider) return queryProvider
  if (legacyLonghornPaths.has(pathname) || [...legacyLonghornPaths].some((path) => pathname.startsWith(`${path}/`))) return 'longhorn'
  return undefined
}

function placeholderProvider(id: string): WorkspaceProvider {
  const knownKinds: Record<string, { kind: string; displayName: string; supportLevel: WorkspaceProvider['supportLevel'] }> = {
    longhorn: { kind: 'longhorn', displayName: 'Longhorn', supportLevel: 'managed' },
    'rook-ceph': { kind: 'rook-ceph', displayName: 'Rook / Ceph', supportLevel: 'managed' },
    openebs: { kind: 'openebs', displayName: 'OpenEBS', supportLevel: 'managed' },
  }
  const known = knownKinds[id]
  return {
    id,
    kind: known?.kind ?? 'csi',
    displayName: known?.displayName ?? id,
    supportLevel: known?.supportLevel ?? 'detected',
    capabilities: [],
    health: { status: 'unknown', conditions: [], observedAt: '' },
  }
}

export function providerWorkspaceFromLocation(
  pathname: string,
  search: string,
  providers: WorkspaceProvider[] | undefined,
): WorkspaceProvider | undefined {
  const id = candidateProviderId(pathname, search)
  if (!id) return undefined
  const provider = providers?.find((candidate) => candidate.id === id)
  if (provider) return provider
  // Avoid a sidebar flash while provider discovery is loading. Once a complete
  // provider list arrives, an absent provider falls back to All Storage.
  return providers === undefined ? placeholderProvider(id) : undefined
}

export function workspaceLanding(provider: WorkspaceProvider | undefined): string {
  if (!provider) return '/storage/providers'
  if (provider.kind === 'longhorn' || provider.id === 'longhorn') return '/dashboard'
  return `/storage/providers/${encodeURIComponent(provider.id)}`
}

/** Static superset retained for breadcrumbs and compatibility callers. */
export const navGroups: NavGroup[] = [...allStorageGroups, ...longhornGroups, ...systemGroups, ...adminPageGroups]

export function findNavItem(pathname: string): NavItem | undefined {
  let best: NavItem | undefined
  for (const group of navGroups) {
    for (const item of group.items) {
      const itemPath = item.path.split('?')[0] ?? item.path
      if (pathname === itemPath || (!item.end && pathname.startsWith(`${itemPath}/`))) {
        const bestPath = best ? (best.path.split('?')[0] ?? best.path) : ''
        if (!best || itemPath.length > bestPath.length) best = item
      }
    }
  }
  const cephKind = pathname.match(/\/ceph\/(quorum|osds|pools|filesystems|mirroring|rbd-images)(?:\/|$)/)?.[1]
  const cephLabels: Record<string, Pick<NavItem, 'id' | 'labelKey' | 'label' | 'icon'>> = {
    quorum: { id: 'ceph-quorum', labelKey: 'nav.quorum', label: 'MON Quorum', icon: Shield },
    osds: { id: 'ceph-osds', labelKey: 'nav.osds', label: 'OSDs', icon: Server },
    pools: { id: 'ceph-pools', labelKey: 'nav.pools', label: 'Block Pools', icon: Layers3 },
    filesystems: { id: 'ceph-filesystems', labelKey: 'nav.filesystems', label: 'CephFS Filesystems', icon: Database },
    mirroring: { id: 'ceph-mirroring', labelKey: 'nav.mirroring', label: 'RBD Mirroring', icon: Network },
    'rbd-images': { id: 'ceph-rbd-images', labelKey: 'nav.rbdImages', label: 'RBD Images', icon: HardDrive },
  }
  const cephLabel = cephKind ? cephLabels[cephKind] : undefined
  if (cephLabel) return { ...cephLabel, path: pathname }
  const openEBSKind = pathname.match(/^\/storage\/providers\/[^/]+\/openebs\/([^/]+)(?:\/|$)/)?.[1]
  if (openEBSKind) {
    const label = openEBSKind.replaceAll('-', ' ').replace(/\b\w/g, (character) => character.toUpperCase())
    return { id: `openebs-${openEBSKind}`, labelKey: `nav.${openEBSKind}`, label, icon: Database, path: pathname }
  }
  return best
}
