import { useQuery } from '@tanstack/react-query'
import { highlandGet } from '@/api/client'
import { useSseConnected } from '@/api/realtime'
import { buildInsightQuery } from './insights'
import type { StorageCondition } from './types'

export type RelationshipConfidence = 'authoritative' | 'derived' | 'potential' | 'unknown'
export type RelationshipFreshness = 'fresh' | 'aging' | 'stale' | 'unknown'

export type GraphNode = {
  apiVersion: string
  id: string
  kind: string
  providerId: string
  namespace?: string
  name: string
  kubernetesUid?: string
  providerRef?: { kind: string; id: string }
  observedAt?: string
  freshness: RelationshipFreshness
  attributes?: Record<string, unknown>
  conditions?: StorageCondition[]
}

export type GraphEdge = {
  apiVersion: string
  id: string
  type: string
  from: string
  to: string
  confidence: RelationshipConfidence
  evidence: Array<{
    source: string
    ref?: string
    observedAt?: string
    freshness: RelationshipFreshness
    confidence: RelationshipConfidence
    message?: string
  }>
}

export type RelationshipGraph = {
  apiVersion: string
  providerId: string
  nodes: GraphNode[]
  edges: GraphEdge[]
  page: { limit: number; continue?: string; total: number }
  observedAt: string
  conditions?: StorageCondition[]
  incomplete?: boolean
}

export type ImpactResult = {
  apiVersion: string
  providerId: string
  target: GraphNode
  confirmed: ImpactResource[]
  potential: ImpactResource[]
  unknown: ImpactResource[]
  backedBy: ImpactResource[]
  summary: {
    requestedCapacity?: string
    provisionedCapacity?: string
    workloadCount: number
    podCount: number
    namespaceCount: number
    snapshotCount: number
    operationCount: number
    attachedCount: number
    detachedCount: number
    accessModes?: string[]
    reclaimPolicies?: string[]
  }
  observedAt: string
  freshness: RelationshipFreshness
  conditions?: StorageCondition[]
  incomplete?: boolean
}

export type ImpactResource = {
  node: GraphNode
  confidence: RelationshipConfidence
  path?: string[]
}

export type DriftReport = {
  apiVersion: string
  providerId: string
  data: Array<{
    id: string
    providerId: string
    category: string
    resource: GraphNode
    firstObserved: string
    lastObserved: string
    duration: string
    severity: 'ok' | 'info' | 'warning' | 'error' | 'unknown'
    actionable: boolean
    actionSurface: string
    suppressed?: boolean
    message: string
  }>
  page: { limit: number; continue?: string; total: number }
  summary: { total: number; error: number; warning: number; info: number; suppressed: number }
  observedAt: string
  conditions?: StorageCondition[]
  incomplete?: boolean
}

export type RelationshipQuery = {
  provider: string
  kind: string
  namespace?: string
  depth?: number
  limit?: number
}

export function canonicalGraphId(kind: string, provider: string, namespace: string, name: string) {
  const encode = (value: string) => {
    const bytes = new TextEncoder().encode(value)
    let binary = ''
    for (const byte of bytes) binary += String.fromCharCode(byte)
    return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '')
  }
  return `v1:${kind}:${encode(provider)}:${encode(namespace)}:${encode(name)}`
}

export const storageContextClient = {
  relationships: (query: RelationshipQuery, signal?: AbortSignal) =>
    highlandGet<RelationshipGraph>(
      `/storage/relationships${buildInsightQuery({
        provider: query.provider,
        kind: query.kind,
        namespace: query.namespace,
        depth: query.depth,
        limit: query.limit,
      })}`, { signal },
    ),
  resourceRelationships: (kind: string, id: string, provider: string, depth = 4, signal?: AbortSignal) =>
    highlandGet<RelationshipGraph>(
      `/storage/resources/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/relationships${buildInsightQuery({
        provider,
        depth,
        limit: 200,
      })}`, { signal },
    ),
  impact: (provider: string, kind: string, id: string, depth = 5, signal?: AbortSignal) =>
    highlandGet<ImpactResult>(
      `/storage/impact${buildInsightQuery({ provider, kind, id, depth })}`, { signal },
    ),
  drift: (provider: string, limit = 100, signal?: AbortSignal) =>
    highlandGet<DriftReport>(
      `/providers/${encodeURIComponent(provider)}/drift${buildInsightQuery({ limit })}`, { signal },
    ),
}

const contextKeys = {
  root: ['storage', 'context'] as const,
  relationships: (query: RelationshipQuery) => [...contextKeys.root, 'relationships', query] as const,
  resource: (provider: string, kind: string, id: string) =>
    [...contextKeys.root, 'resource', provider, kind, id] as const,
  impact: (provider: string, kind: string, id: string) =>
    [...contextKeys.root, 'impact', provider, kind, id] as const,
  drift: (provider: string) => [...contextKeys.root, 'drift', provider] as const,
}

export function useRelationships(query: RelationshipQuery) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: contextKeys.relationships(query),
    queryFn: ({ signal }) => storageContextClient.relationships(query, signal),
    enabled: Boolean(query.provider && query.kind),
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useResourceRelationships(provider: string, kind: string, id: string) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: contextKeys.resource(provider, kind, id),
    queryFn: ({ signal }) => storageContextClient.resourceRelationships(kind, id, provider, 4, signal),
    enabled: Boolean(provider && kind && id),
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useStorageImpact(provider: string, kind: string, id: string) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: contextKeys.impact(provider, kind, id),
    queryFn: ({ signal }) => storageContextClient.impact(provider, kind, id, 5, signal),
    enabled: Boolean(provider && kind && id),
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useProviderDrift(provider: string) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: contextKeys.drift(provider),
    queryFn: ({ signal }) => storageContextClient.drift(provider, 100, signal),
    enabled: Boolean(provider),
    refetchInterval: connected ? 60_000 : 30_000,
  })
}
