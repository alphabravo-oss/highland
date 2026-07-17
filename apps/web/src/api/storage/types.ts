type SupportLevel = 'detected' | 'verified' | 'managed'
type Severity = 'ok' | 'info' | 'warning' | 'error' | 'unknown'
type Capability = string

export type StorageCondition = {
  type: string
  status: string
  severity: Severity
  reason?: string
  message?: string
  lastTransitionTime?: string
  observedAt?: string
}

export type ProviderHealth = {
  status: Severity
  conditions: StorageCondition[]
  observedAt: string
  stale?: boolean
}

export type ProviderDescriptor = {
  id: string
  kind: string
  displayName: string
  supportLevel: SupportLevel
  drivers: string[]
  version?: string
  namespace?: string
  capabilities: Capability[]
  resourceKinds?: string[]
  health: ProviderHealth
  metadata?: Record<string, string>
}

export type DriverSummary = {
  name: string
  providerId: string
  supportLevel: SupportLevel
  nodeCount: number
  storageClassCount: number
  persistentVolumeCount: number
  attachRequired?: boolean
  storageCapacity?: boolean
}

export type StorageClassSummary = {
  name: string
  kubernetesUid: string
  providerId: string
  provisioner: string
  reclaimPolicy: string
  volumeBindingMode: string
  allowVolumeExpansion: boolean
  default: boolean
  parameters?: Record<string, string>
  claimCount: number
  volumeCount: number
  snapshotClasses?: string[]
  conditions?: StorageCondition[]
}

type WorkloadReference = {
  namespace: string
  kind: string
  name: string
  podName: string
  podPhase: string
  nodeName?: string
}

export type ClaimSummary = {
  id: string
  namespace: string
  name: string
  kubernetesUid: string
  providerId: string
  driver?: string
  storageClass?: string
  pvName?: string
  phase: string
  requestedCapacity?: string
  provisionedCapacity?: string
  accessModes?: string[]
  volumeMode?: string
  volumeHandle?: string
  reclaimPolicy?: string
  workloads?: WorkloadReference[]
  attachmentIds?: string[]
  providerRef?: { kind: string; id: string }
  conditions?: StorageCondition[]
}

export type PersistentVolumeSummary = {
  name: string
  kubernetesUid: string
  providerId: string
  driver?: string
  volumeHandle?: string
  storageClass?: string
  phase: string
  capacity?: string
  reclaimPolicy?: string
  claimNamespace?: string
  claimName?: string
  attachmentIds?: string[]
  providerRef?: { kind: string; id: string }
  backendAllocatedCapacity?: string
  backend?: Record<string, unknown>
  conditions?: StorageCondition[]
}

export type SnapshotSummary = {
  id: string
  namespace: string
  name: string
  kubernetesUid: string
  providerId: string
  driver?: string
  snapshotClass?: string
  sourcePvc?: string
  readyToUse?: boolean
  restoreSize?: string
  deletionPolicy?: string
  conditions?: StorageCondition[]
}

export type AttachmentSummary = {
  name: string
  providerId: string
  driver: string
  pvName: string
  nodeName: string
  attached: boolean
  attachError?: string
  detachError?: string
  conditions?: StorageCondition[]
}

export type CapacitySummary = {
  providerId: string
  driver: string
  storageClass: string
  capacity: string
  maximumVolumeSize?: string
  observedAt: string
}

export type StorageEvent = {
  namespace?: string
  name: string
  type?: string
  reason?: string
  message?: string
  regardingKind?: string
  regardingName?: string
  count?: number
  lastObservedAt?: string
}

type PageMeta = { limit: number; continue?: string; total: number }
type ResponseMeta = { observedAt: string; stale: boolean; partial: boolean; requestId?: string }
export type StoragePage<T> = { data: T[]; page: PageMeta; meta?: ResponseMeta; conditions?: StorageCondition[] }
export type ProviderList = {
  data: ProviderDescriptor[]
  meta: { lastSync: string; snapshotApi: boolean; observedAt?: string; stale?: boolean; partial?: boolean; requestId?: string; conditions?: StorageCondition[] }
}

export type StorageFilters = {
  provider?: string
  driver?: string
  namespace?: string
  status?: string
  search?: string
  limit?: number
  continue?: string
}

type StorageAction = {
  id: string
  capability: string
  minimumRole: 'operator' | 'admin'
  providerKind?: string
  risk: 'low' | 'medium' | 'high' | 'critical'
  confirmation: 'summary' | 'typed-name'
  featureFlag: string
  preflightChecks: string[]
  auditAction: string
}
export type ActionAvailability = { action: StorageAction; enabled: boolean; available: boolean; unavailableReason?: string }
type OperationTarget = { apiVersion?: string; kind: string; namespace?: string; name: string; uid?: string; resourceVersion?: string }
export type OperationRequest = { actionId: string; providerId?: string; target: OperationTarget; parameters?: Record<string, unknown>; confirmation?: { challenge: string; typedName?: string; warningsAcknowledged?: boolean } }
export type OperationPlan = {
  action: StorageAction
  providerId?: string
  target: OperationTarget
  resources: Array<{ apiVersion: string; kind: string; namespace?: string; name: string; operation: string; manifest?: Record<string, unknown> }>
  dependencies?: OperationTarget[]
  checks: Array<{ id: string; status: string; message: string }>
  warnings?: string[]
  blastRadius: string
  hash: string
  challenge: string
  challengeExpiresAt: string
  observedAt: string
}
export type StorageOperation = {
  apiVersion: string
  kind: string
  name: string
  creationTimestamp: string
  spec: { actionId: string; providerId?: string; target: OperationTarget; requester: string; requesterRole: string; planHash: string; requestedAt: string }
  status: { phase: string; step?: string; conditions?: Array<{ type: string; reason?: string; message?: string; lastTransitionTime: string }>; startedAt?: string; finishedAt?: string; retries?: number; errorCode?: string; diagnostics?: string }
}
