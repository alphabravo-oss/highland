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
  providers: (signal?: AbortSignal) => highlandGet<ProviderList>('/storage/providers', { signal }),
  provider: (id: string, signal?: AbortSignal) => highlandGet<ProviderDescriptor>(`/storage/providers/${encodeURIComponent(id)}`, { signal }),
  summary: <T>(id: string, signal?: AbortSignal) => highlandGet<T>(`/providers/${encodeURIComponent(id)}/summary`, { signal }),
  drivers: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<DriverSummary>>(`/storage/drivers${query(filters)}`, { signal }),
  classes: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<StorageClassSummary>>(`/storage/classes${query(filters)}`, { signal }),
  claims: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<ClaimSummary>>(`/storage/claims${query(filters)}`, { signal }),
  claim: (namespace: string, name: string, signal?: AbortSignal) => highlandGet<ClaimSummary>(`/storage/claims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, { signal }),
  volumes: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<PersistentVolumeSummary>>(`/storage/volumes${query(filters)}`, { signal }),
  volume: (name: string, signal?: AbortSignal) => highlandGet<PersistentVolumeSummary>(`/storage/volumes/${encodeURIComponent(name)}`, { signal }),
  snapshots: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<SnapshotSummary>>(`/storage/snapshots${query(filters)}`, { signal }),
  attachments: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<AttachmentSummary>>(`/storage/attachments${query(filters)}`, { signal }),
  capacity: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<CapacitySummary>>(`/storage/capacity${query(filters)}`, { signal }),
  events: (filters?: StorageFilters, signal?: AbortSignal) => highlandGet<StoragePage<StorageEvent>>(`/storage/events${query(filters)}`, { signal }),
  resources: <T>(providerId: string, kind: string, filters?: StorageFilters, signal?: AbortSignal) =>
    highlandGet<StoragePage<T>>(`/providers/${encodeURIComponent(providerId)}/resources/${encodeURIComponent(kind)}${query(filters)}`, { signal }),
  resource: <T>(providerId: string, kind: string, id: string, signal?: AbortSignal) =>
    highlandGet<T>(`/providers/${encodeURIComponent(providerId)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`, { signal }),
  actions: (signal?: AbortSignal) => highlandGet<{ data: ActionAvailability[]; writesEnabled: boolean; portableProviderIds: string[]; policySource?: string; policyGeneration?: number }>('/storage/actions', { signal }),
  operations: (filters?: StorageFilters & { action?: string; state?: string; user?: string }, signal?: AbortSignal) => highlandGet<{ data: StorageOperation[] }>(`/storage/operations${query(filters)}`, { signal }),
  operation: (id: string, signal?: AbortSignal) => highlandGet<StorageOperation>(`/storage/operations/${encodeURIComponent(id)}`, { signal }),
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
    const route = request.actionId.startsWith('longhorn-')
      ? [`/providers/${provider}/longhorn/operations/${encodeURIComponent(request.actionId)}`, 'POST'] as [string, string]
      : routes[request.actionId]
    if (!route) throw new Error('Unsupported storage action')
    return highlandRequest<{ operationId: string; operation: StorageOperation }>(route[0], route[1], request)
  },
}
