import { highlandGet, highlandPost, highlandRequest } from '@/api/client'
import type {
  AttachmentSummary,
  CapacitySummary,
  ClaimSummary,
  DriverSummary,
  PersistentVolumeSummary,
  ProviderDescriptor,
  ProviderList,
  SnapshotSummary,
  StorageClassSummary,
  StorageEvent,
  StorageFilters,
  StoragePage,
  ActionAvailability,
  OperationPlan,
  OperationRequest,
  StorageOperation,
} from './types'

function query(filters: StorageFilters = {}): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

export const storageClient = {
  revealCephDashboardCredential: () => highlandPost<{ username: string; password: string }>('/admin/providers/rook-ceph/dashboard-credential/reveal'),
  providers: () => highlandGet<ProviderList>('/storage/providers'),
  provider: (id: string) => highlandGet<ProviderDescriptor>(`/storage/providers/${encodeURIComponent(id)}`),
  summary: <T>(id: string) => highlandGet<T>(`/providers/${encodeURIComponent(id)}/summary`),
  drivers: (filters?: StorageFilters) => highlandGet<StoragePage<DriverSummary>>(`/storage/drivers${query(filters)}`),
  classes: (filters?: StorageFilters) => highlandGet<StoragePage<StorageClassSummary>>(`/storage/classes${query(filters)}`),
  claims: (filters?: StorageFilters) => highlandGet<StoragePage<ClaimSummary>>(`/storage/claims${query(filters)}`),
  claim: (namespace: string, name: string) => highlandGet<ClaimSummary>(`/storage/claims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`),
  volumes: (filters?: StorageFilters) => highlandGet<StoragePage<PersistentVolumeSummary>>(`/storage/volumes${query(filters)}`),
  volume: (name: string) => highlandGet<PersistentVolumeSummary>(`/storage/volumes/${encodeURIComponent(name)}`),
  snapshots: (filters?: StorageFilters) => highlandGet<StoragePage<SnapshotSummary>>(`/storage/snapshots${query(filters)}`),
  attachments: (filters?: StorageFilters) => highlandGet<StoragePage<AttachmentSummary>>(`/storage/attachments${query(filters)}`),
  capacity: (filters?: StorageFilters) => highlandGet<StoragePage<CapacitySummary>>(`/storage/capacity${query(filters)}`),
  events: (filters?: StorageFilters) => highlandGet<StoragePage<StorageEvent>>(`/storage/events${query(filters)}`),
  resources: <T>(providerId: string, kind: string, filters?: StorageFilters) =>
    highlandGet<StoragePage<T>>(`/providers/${encodeURIComponent(providerId)}/resources/${encodeURIComponent(kind)}${query(filters)}`),
  resource: <T>(providerId: string, kind: string, id: string) =>
    highlandGet<T>(`/providers/${encodeURIComponent(providerId)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`),
  actions: () => highlandGet<{ data: ActionAvailability[]; writesEnabled: boolean }>('/storage/actions'),
  operations: (filters?: StorageFilters & { action?: string; state?: string; user?: string }) => highlandGet<{ data: StorageOperation[] }>(`/storage/operations${query(filters)}`),
  operation: (id: string) => highlandGet<StorageOperation>(`/storage/operations/${encodeURIComponent(id)}`),
  plan: (request: OperationRequest) => highlandPost<OperationPlan>('/storage/plans', request),
  submit: (request: OperationRequest) => {
    const ns = encodeURIComponent(request.target.namespace ?? '')
    const name = encodeURIComponent(request.target.name)
    const provider = encodeURIComponent(request.providerId ?? 'rook-ceph')
    const routes: Record<string, [string, string]> = {
      'create-pvc': ['/storage/claims', 'POST'], 'expand-pvc': [`/storage/claims/${ns}/${name}/size`, 'PATCH'], 'delete-pvc': [`/storage/claims/${ns}/${name}`, 'DELETE'],
      'create-snapshot': ['/storage/snapshots', 'POST'], 'delete-snapshot': [`/storage/snapshots/${ns}/${name}`, 'DELETE'], 'restore-snapshot': ['/storage/restores', 'POST'], 'clone-pvc': ['/storage/clones', 'POST'],
      'create-ceph-rbd-storageclass': [`/providers/${provider}/ceph/storage-classes`, 'POST'], 'create-cephfs-storageclass': [`/providers/${provider}/ceph/storage-classes`, 'POST'], 'delete-ceph-storageclass': [`/providers/${provider}/ceph/storage-classes/${name}`, 'DELETE'],
      'create-ceph-blockpool': [`/providers/${provider}/ceph/block-pools`, 'POST'], 'delete-ceph-blockpool': [`/providers/${provider}/ceph/block-pools/${ns}/${name}`, 'DELETE'],
    }
    const route = routes[request.actionId]
    if (!route) throw new Error('Unsupported storage action')
    return highlandRequest<{ operationId: string; operation: StorageOperation }>(route[0], route[1], request)
  },
}
