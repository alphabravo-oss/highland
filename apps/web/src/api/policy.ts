import { highlandGet, highlandPost, highlandPut } from './client'

export type StorageWritePolicy = {
  acceptNewOperations: boolean
  portableKubernetesWrites: boolean
  portableKubernetesProviderIds: string[]
  longhornWrites: boolean
  rookCephWrites: boolean
  allowCephStorageClassDelete: boolean
  allowCephPoolDelete: boolean
}

export type StoragePolicyBooleanField = Exclude<keyof StorageWritePolicy, 'portableKubernetesProviderIds'>

const disabledStoragePolicy: StorageWritePolicy = {
  acceptNewOperations: false,
  portableKubernetesWrites: false,
  portableKubernetesProviderIds: [],
  longhornWrites: false,
  rookCephWrites: false,
  allowCephStorageClassDelete: false,
  allowCephPoolDelete: false,
}

export type StoragePolicyPreset = 'disabled' | 'longhorn-native-only' | 'longhorn-full'

export function storagePolicyPreset(preset: StoragePolicyPreset): StorageWritePolicy {
  if (preset === 'longhorn-native-only') return { ...disabledStoragePolicy, portableKubernetesProviderIds: [], acceptNewOperations: true, longhornWrites: true }
  if (preset === 'longhorn-full') return { ...disabledStoragePolicy, portableKubernetesProviderIds: ['longhorn'], acceptNewOperations: true, portableKubernetesWrites: true, longhornWrites: true }
  return { ...disabledStoragePolicy }
}

export function updatePolicyField(current: StorageWritePolicy, key: StoragePolicyBooleanField, checked: boolean): StorageWritePolicy {
  const next = { ...current, [key]: checked }
  if (key === 'acceptNewOperations' && !checked) return { ...disabledStoragePolicy, portableKubernetesProviderIds: [] }
  if (key === 'portableKubernetesWrites' && !checked) next.portableKubernetesProviderIds = []
  if (key === 'rookCephWrites' && !checked) {
    next.allowCephStorageClassDelete = false
    next.allowCephPoolDelete = false
  }
  return next
}

type PolicyCeiling = Omit<StorageWritePolicy, 'acceptNewOperations' | 'portableKubernetesProviderIds'> & { portableKubernetesProviderIds?: string[] }

type PolicyCondition = {
  type: string
  status: string
  reason?: string
  message?: string
  lastTransitionTime: string
}

export type StoragePolicyResponse = {
  requested: StorageWritePolicy
  effective: StorageWritePolicy
  ceiling: PolicyCeiling
  conditions: PolicyCondition[]
  source: 'runtime-policy' | 'static-helm' | 'unavailable'
  generation: number
  resourceVersion: string
  observedGeneration: number
  inFlightOperations: number
  lastChange?: { username?: string; requestId?: string; at?: string }
  meta: { observedAt: string; stale: boolean; partial: boolean; requestId?: string }
}

export type StoragePolicyPlan = {
  current: StorageWritePolicy
  requested: StorageWritePolicy
  effective: StorageWritePolicy
  ceiling: PolicyCeiling
  conditions: PolicyCondition[]
  resourceVersion: string
  policyGeneration: number
  broadening: boolean
  enablesCephPoolDelete: boolean
  impact: {
    actionIds: string[]
    roles: string[]
    addedPortableProviderIds: string[]
    removedPortableProviderIds: string[]
  }
  inFlightOperations: number
  clusterIdentity: string
  actor: string
  requestId: string
  hash: string
  challenge: string
  challengeExpiresAt: string
  observedAt: string
}

export type PolicyChangeRequest = {
  policy: StorageWritePolicy
  resourceVersion: string
  confirmation?: {
    challenge: string
    clusterIdentity?: string
    enablePhrase?: string
    cephPoolPhrase?: string
    impactAcknowledged?: boolean
  }
}

export type PolicyHistoryResponse = {
  data: Array<Record<string, unknown>>
  page: { limit: number; total: number }
  meta: { observedAt: string; stale: boolean; partial: boolean; requestId?: string }
}

export const storagePolicyClient = {
  get: (signal?: AbortSignal) => highlandGet<StoragePolicyResponse>('/admin/storage-policy', { signal }),
  plan: (request: PolicyChangeRequest) => highlandPost<StoragePolicyPlan>('/admin/storage-policy/plans', request),
  apply: (request: PolicyChangeRequest) => highlandPut<StoragePolicyResponse>('/admin/storage-policy', request),
  history: (signal?: AbortSignal) => highlandGet<PolicyHistoryResponse>('/admin/storage-policy/history', { signal }),
}
