import { describe, expect, it } from 'vitest'
import type { WorkspaceProvider } from './nav'
import {
  findNavItem,
  navigationForWorkspace,
  navGroups,
  providerWorkspaceFromLocation,
  workspaceLanding,
} from './nav'

const longhorn: WorkspaceProvider = {
  id: 'longhorn', kind: 'longhorn', displayName: 'Longhorn', supportLevel: 'managed',
  capabilities: ['inventory.claims.read', 'inventory.volumes.read', 'inventory.snapshots.read'],
  health: { status: 'ok', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
}
const rook: WorkspaceProvider = {
  id: 'rook-ceph', kind: 'rook-ceph', displayName: 'Rook / Ceph', supportLevel: 'managed',
  capabilities: ['inventory.claims.read', 'inventory.volumes.read', 'inventory.snapshots.read', 'inventory.attachments.read', 'inventory.capacity.read', 'inventory.events.read'],
  health: { status: 'ok', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
  metadata: { dashboard: 'configured' },
}
const generic: WorkspaceProvider = {
  id: 'csi-example', kind: 'csi', displayName: 'example.csi.io', supportLevel: 'detected',
  capabilities: ['inventory.claims.read', 'inventory.volumes.read', 'inventory.capacity.read'],
  health: { status: 'unknown', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
}
const openebs: WorkspaceProvider = {
  id: 'openebs', kind: 'openebs', displayName: 'OpenEBS', supportLevel: 'managed',
  capabilities: ['inventory.claims.read', 'inventory.volumes.read', 'inventory.snapshots.read', 'inventory.capacity.read'],
  health: { status: 'ok', conditions: [], observedAt: '2026-07-16T00:00:00Z' },
  metadata: {
    'engine.hostpath': 'true',
    'engine.lvm': 'false',
    'engine.zfs': 'true',
    'engine.mayastor': 'false',
  },
}

const itemIds = (groups: ReturnType<typeof navigationForWorkspace>) => groups.flatMap((group) => group.items.map((item) => item.id))

describe('workspace navigation', () => {
  it('keeps a static navigation superset for breadcrumbs with safe fallback labels', () => {
    expect(navGroups.length).toBeGreaterThan(0)
    for (const group of navGroups) {
      expect(group.labelKey.startsWith('nav.')).toBe(true)
      expect(group.label).toBeTruthy()
      for (const item of group.items) {
        expect(item.path.startsWith('/')).toBe(true)
        expect(item.icon).toBeTruthy()
        expect(item.labelKey.startsWith('nav.')).toBe(true)
        expect(item.label).toBeTruthy()
      }
    }
  })

  it('shows a compact cross-provider workspace without Longhorn internals', () => {
    const ids = itemIds(navigationForWorkspace(undefined, 'admin'))
    expect(ids).toEqual(expect.arrayContaining(['storage-providers', 'storage-inventory', 'storage-operations', 'storage-events', 'status', 'admin']))
    expect(ids).not.toEqual(expect.arrayContaining(['admin-users', 'sso', 'audit']))
    expect(ids).not.toContain('volumes')
    expect(ids).not.toContain('backups')
  })

  it('shows the legacy operational tree only in the Longhorn workspace', () => {
    const groups = navigationForWorkspace(longhorn, 'admin')
    const ids = itemIds(groups)
    expect(ids).toEqual(expect.arrayContaining(['longhorn-dashboard', 'longhorn-provider-details', 'volumes', 'nodes', 'backups', 'backing-images', 'support-bundle', 'settings']))
    expect(groups.flatMap((group) => group.items).find((item) => item.id === 'longhorn-provider-details')?.label).toBe('Provider details')
    expect(ids.some((id) => id.endsWith('-pools'))).toBe(false)
    expect(ids).not.toContain('storage-providers')
  })

  it('shows Ceph resources and provider-filtered Kubernetes inventory only for Rook/Ceph', () => {
    const groups = navigationForWorkspace(rook, 'operator')
    const ids = itemIds(groups)
    expect(ids).toEqual(expect.arrayContaining(['rook-ceph-dashboard', 'rook-ceph-pools', 'rook-ceph-osds', 'rook-ceph-filesystems', 'rook-ceph-rbd-images', 'rook-ceph-claims']))
    expect(groups.flatMap((group) => group.items).find((item) => item.id === 'rook-ceph-dashboard')?.label).toBe('Dashboard')
    expect(ids).not.toContain('volumes')
    expect(ids).not.toContain('backups')
    const claims = groups.flatMap((group) => group.items).find((item) => item.id === 'rook-ceph-claims')
    expect(claims?.path).toBe('/storage/claims?provider=rook-ceph')
  })

  it('keeps a detected CSI workspace capability-scoped', () => {
    const ids = itemIds(navigationForWorkspace(generic, 'viewer'))
    expect(ids).toEqual(expect.arrayContaining(['csi-example-dashboard', 'csi-example-classes', 'csi-example-claims', 'csi-example-volumes', 'csi-example-capacity']))
    expect(ids).not.toContain('csi-example-snapshots')
    expect(ids).not.toContain('admin')
  })

  it('keeps OpenEBS engine navigation separate and hides engines that are not installed', () => {
    const groups = navigationForWorkspace(openebs, 'operator')
    const ids = itemIds(groups)
    expect(ids).toEqual(expect.arrayContaining([
      'openebs-dashboard', 'openebs-components', 'openebs-hostpath-volumes', 'openebs-zfs-nodes',
      'openebs-zfs-volumes', 'openebs-zfs-snapshots', 'openebs-claims',
    ]))
    expect(groups.flatMap((group) => group.items).find((item) => item.id === 'openebs-dashboard')?.label).toBe('Dashboard')
    expect(ids).not.toContain('openebs-disk-pools')
    expect(ids).not.toContain('openebs-lvm-volumes')
    expect(groups.find((group) => group.id === 'openebs-hostpath')?.label).toBe('HostPath')
    expect(groups.find((group) => group.id === 'openebs-zfs')?.label).toBe('LocalPV ZFS')
  })

  it('gives LINSTOR a backend-specific lifecycle and placement workspace', () => {
    const provider = { ...openebs, id: 'linstor', kind: 'linstor', displayName: 'Piraeus / LINSTOR' }
    const ids = navigationForWorkspace(provider, 'viewer').flatMap((group) => group.items.map((item) => item.id))
    expect(ids).toEqual(expect.arrayContaining([
      'linstor-dashboard', 'linstor-components', 'linstor-nodes', 'linstor-storage-pools',
      'linstor-resource-groups', 'linstor-replicas', 'linstor-snapshots', 'linstor-remotes',
      'linstor-clusters', 'linstor-satellites', 'linstor-claims',
    ]))
    expect(ids).not.toContain('openebs-disk-pools')
    expect(ids).not.toContain('rook-ceph-osds')
  })

  it('resolves workspace context from provider routes, filters, and legacy Longhorn routes', () => {
    const providers = [longhorn, rook, generic]
    expect(providerWorkspaceFromLocation('/storage/providers/rook-ceph/ceph/pools', '', providers)?.id).toBe('rook-ceph')
    expect(providerWorkspaceFromLocation('/storage/claims', '?provider=csi-example', providers)?.id).toBe('csi-example')
    expect(providerWorkspaceFromLocation('/volumes/volume-a', '', providers)?.id).toBe('longhorn')
    expect(providerWorkspaceFromLocation('/storage/providers', '', providers)).toBeUndefined()
    expect(providerWorkspaceFromLocation('/dashboard', '', [rook])).toBeUndefined()
  })

  it('uses a loading placeholder to avoid flashing the wrong workspace', () => {
    expect(providerWorkspaceFromLocation('/dashboard', '', undefined)?.id).toBe('longhorn')
    expect(providerWorkspaceFromLocation('/storage/providers/new-csi', '', undefined)?.kind).toBe('csi')
    expect(providerWorkspaceFromLocation('/storage/providers/openebs/openebs/hostpath-volumes', '', undefined)?.kind).toBe('openebs')
  })

  it('returns the appropriate workspace landing', () => {
    expect(workspaceLanding(undefined)).toBe('/storage/providers')
    expect(workspaceLanding(longhorn)).toBe('/dashboard')
    expect(workspaceLanding(rook)).toBe('/storage/providers/rook-ceph')
  })

  it('filters admin groups by role and resolves static/ceph breadcrumbs', () => {
    expect(itemIds(navigationForWorkspace(undefined, 'viewer'))).not.toContain('admin')
    expect(findNavItem('/admin')?.label).toBe('Administration')
    expect(findNavItem('/admin/users')?.label).toBe('Users')
    expect(findNavItem('/volumes/vol-1')?.id).toBe('volumes')
    expect(findNavItem('/storage/providers/longhorn')?.label).toBe('Provider details')
    expect(findNavItem('/storage/providers/rook-ceph')?.label).toBe('Dashboard')
    expect(findNavItem('/storage/providers/openebs')?.label).toBe('Dashboard')
    expect(findNavItem('/storage/providers/csi-example')?.label).toBe('Dashboard')
    expect(findNavItem('/storage/providers/rook-ceph/ceph/pools/replicapool')?.label).toBe('Block Pools')
    expect(findNavItem('/storage/providers/openebs/openebs/hostpath-volumes')?.label).toBe('Hostpath Volumes')
  })
})
