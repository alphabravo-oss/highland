import { useQuery } from '@tanstack/react-query'
import { highlandGet } from '@/api/client'
import { useSseConnected } from '@/api/realtime'

export type EvidenceStrength = 'authoritative' | 'derived' | 'potential' | 'unknown'
export type TimelineSource =
  | 'kubernetes-event'
  | 'rook-condition'
  | 'ceph-health'
  | 'provider'
  | 'storage-operation'
  | 'audit'
  | 'configuration'
export type TimelineSeverity = 'info' | 'warning' | 'error' | 'critical' | 'unknown'
export type TimelineOrdering = 'known' | 'clock-skew' | 'unknown'
export type RetentionClass = 'transient' | 'durable' | 'audit'

export type InsightCondition = {
  code: string
  message: string
}

export type ResourceIdentity = {
  apiVersion?: string
  kind: string
  namespace?: string
  name: string
  uid?: string
}

export type WorkloadIdentity = {
  kind: string
  namespace: string
  name: string
  uid?: string
}

export type TimelineLink = {
  kind: string
  href: string
}

export type TimelineEntry = {
  id: string
  providerId?: string
  namespace?: string
  workload?: WorkloadIdentity
  resource?: ResourceIdentity
  severity: TimelineSeverity
  source: TimelineSource
  action?: string
  reason?: string
  message?: string
  count: number
  firstOccurredAt?: string
  lastOccurredAt?: string
  observedAt: string
  ordering: TimelineOrdering
  /** Nanoseconds when serialized directly from Go's time.Duration. */
  clockSkew?: number
  attribution: {
    providerId?: string
    evidence: EvidenceStrength
    reason?: string
  }
  links?: TimelineLink[]
  retention: RetentionClass
}

export type StorageTimeline = {
  entries: TimelineEntry[]
  total: number
  truncated?: boolean
  conditions?: InsightCondition[]
}

export type TimelineQuery = {
  provider?: string
  namespaces?: string[]
  workload?: string
  resource?: string
  severities?: TimelineSeverity[]
  sources?: TimelineSource[]
  actions?: string[]
  since?: string
  until?: string
  limit?: number
}

export type CapacityMeasure =
  | 'pvc-requested'
  | 'pv-provisioned'
  | 'backend-logical'
  | 'backend-allocated'
  | 'pool-usable'
  | 'pool-raw'
  | 'cluster-raw'

/** Bytes may be encoded as a decimal string to preserve uint64 precision. */
export type ByteValue = number | string

export type CapacityDimensions = {
  providerId: string
  driver?: string
  storageClass?: string
  namespace?: string
  workloadKind?: string
  workload?: string
  reclaimPolicy?: string
  pool?: string
  filesystem?: string
}

export type CapacityOwnershipGroup = {
  measure: CapacityMeasure
  bytes: ByteValue
  dimensions: CapacityDimensions
  observations: number
  oldestAt?: string
  newestAt?: string
  evidence: EvidenceStrength[]
}

export type CapacityOwnership = {
  groups: CapacityOwnershipGroup[]
  conditions?: InsightCondition[]
  observedAt: string
}

export type CapacityOwnershipQuery = {
  provider?: string
  namespaces?: string[]
  measures?: CapacityMeasure[]
  authoritativeOnly?: boolean
  limit?: number
}

export type ForecastStatus = 'available' | 'unavailable'
export type ForecastConfidence = 'low' | 'medium' | 'high'

export type CapacityForecast = {
  providerId: string
  measure: CapacityMeasure
  status: ForecastStatus
  currentBytes?: ByteValue
  slopeBytesPerDay?: number
  projectedBytes?: ByteValue
  projectionAt?: string
  sampleCount: number
  /** Nanoseconds when serialized directly from Go's time.Duration. */
  window: number
  latestSampleAt?: string
  rSquared?: number
  confidence?: ForecastConfidence
  conditions?: InsightCondition[]
}

export type CapacityForecastQuery = {
  measure: CapacityMeasure
  horizon?: string
}

type QueryValue = string | number | boolean | readonly string[] | undefined

export function buildInsightQuery(values: Record<string, QueryValue>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value === undefined || value === '') continue
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== '') params.append(key, item)
      }
      continue
    }
    params.set(key, String(value))
  }
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

export const storageInsightClient = {
  timeline: (query: TimelineQuery = {}, signal?: AbortSignal) =>
    highlandGet<StorageTimeline>(
      `/storage/timeline${buildInsightQuery({
        provider: query.provider,
        namespace: query.namespaces,
        workload: query.workload,
        resource: query.resource,
        severity: query.severities,
        source: query.sources,
        action: query.actions,
        since: query.since,
        until: query.until,
        limit: query.limit,
      })}`, { signal },
    ),
  capacityOwnership: (query: CapacityOwnershipQuery = {}, signal?: AbortSignal) =>
    highlandGet<CapacityOwnership>(
      `/storage/capacity/ownership${buildInsightQuery({
        provider: query.provider,
        namespace: query.namespaces,
        measure: query.measures,
        authoritativeOnly: query.authoritativeOnly,
        limit: query.limit,
      })}`, { signal },
    ),
  capacityForecast: (providerId: string, query: CapacityForecastQuery, signal?: AbortSignal) =>
    highlandGet<CapacityForecast>(
      `/providers/${encodeURIComponent(providerId)}/capacity/forecast${buildInsightQuery({
        measure: query.measure,
        horizon: query.horizon,
      })}`, { signal },
    ),
}

export const storageInsightKeys = {
  root: ['storage', 'insights'] as const,
  timeline: (query: TimelineQuery) => [...storageInsightKeys.root, 'timeline', query] as const,
  capacityOwnership: (query: CapacityOwnershipQuery) =>
    [...storageInsightKeys.root, 'capacity-ownership', query] as const,
  capacityForecast: (providerId: string, query: CapacityForecastQuery) =>
    [...storageInsightKeys.root, 'capacity-forecast', providerId, query] as const,
}

export function useStorageTimeline(query: TimelineQuery = {}) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageInsightKeys.timeline(query),
    queryFn: ({ signal }) => storageInsightClient.timeline(query, signal),
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useCapacityOwnership(query: CapacityOwnershipQuery = {}) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageInsightKeys.capacityOwnership(query),
    queryFn: ({ signal }) => storageInsightClient.capacityOwnership(query, signal),
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useCapacityForecast(providerId: string, query: CapacityForecastQuery) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageInsightKeys.capacityForecast(providerId, query),
    queryFn: ({ signal }) => storageInsightClient.capacityForecast(providerId, query, signal),
    enabled: Boolean(providerId && query.measure),
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 120_000 : 60_000,
  })
}
