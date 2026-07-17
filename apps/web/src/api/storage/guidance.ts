import { useQuery } from '@tanstack/react-query'
import { highlandGet } from '@/api/client'
import { useSseConnected } from '@/api/realtime'
import type {
  EvidenceStrength,
  InsightCondition,
  ResourceIdentity,
  TimelineSeverity,
} from './insights'
import { buildInsightQuery } from './insights'

export type ComparisonSupportLevel = 'detected' | 'verified' | 'managed'
export type FactState = 'supported' | 'unsupported' | 'unknown'

export type ComparisonEvidence = {
  source: string
  strength: EvidenceStrength
  observedAt: string
  stale?: boolean
  detail?: string
}

export type CapabilityFact = {
  id: string
  state: FactState
  verified?: boolean
  evidence: ComparisonEvidence
}

export type TestedProfile = {
  providerKind: string
  providerVersion?: string
  driver: string
  driverVersion?: string
  kubernetesVersion?: string
}

export type OperationalSurface = {
  capability: string
  surface: string
  readOnly?: boolean
  detail?: string
}

export type BenchmarkFact = {
  semantic: string
  unit: string
  method: string
  profile: string
  value: number
  evidence: ComparisonEvidence
}

export type PlacementCandidate = {
  providerId: string
  providerName: string
  storageClass: string
  supportLevel: ComparisonSupportLevel
  testedProfile: TestedProfile
  health?: { status: string; evidence: ComparisonEvidence }
  capabilities: CapabilityFact[]
  accessModes?: string[]
  topologyKeys?: string[]
  reclaimPolicy?: string
  headroom?: { percent: number; evidence: ComparisonEvidence }
  benchmarks?: BenchmarkFact[]
  operations?: OperationalSurface[]
}

export type PlacementPolicy = {
  requiredAccessMode?: string
  requiredTopology?: string[]
  requireSnapshot?: boolean
  requireClone?: boolean
  requireEncryption?: boolean
  requireExpansion?: boolean
  requireHealthy?: boolean
  minimumHeadroomPercent?: number
  minimumSupportLevel?: ComparisonSupportLevel
}

export type CriterionResult = {
  criterion: string
  state: FactState
  reason: string
  evidence?: ComparisonEvidence
}

export type CandidateAssessment = {
  candidate: PlacementCandidate
  eligibility: 'eligible' | 'ineligible' | 'unknown'
  criteria: CriterionResult[]
  conditions?: InsightCondition[]
}

export type ProviderComparison = {
  assessments: CandidateAssessment[]
  policy: PlacementPolicy
  conditions?: InsightCondition[]
  observedAt: string
}

export type ComparisonQuery = PlacementPolicy & {
  providers?: string[]
  storageClasses?: string[]
  limit?: number
}

export type ActionSurface =
  | 'highland'
  | 'rook-cr'
  | 'ceph-dashboard'
  | 'ceph-cli'
  | 'runbook'
  | 'observe-only'
export type EscalationLevel = 'operator' | 'admin' | 'storage-specialist' | 'vendor'

export type RemediationEvidence = {
  source: string
  strength: EvidenceStrength
  observedAt: string
  reference?: string
  summary: string
}

export type Remediation = {
  id: string
  conditionCode: string
  providerId: string
  resource?: ResourceIdentity
  title: string
  explanation: string
  surface: ActionSurface
  highlandActionId?: string
  dashboardDestination?: string
  runbookUrl?: string
  prerequisites: string[]
  risks: string[]
  escalation: EscalationLevel
  evidence: RemediationEvidence[]
  fresh: boolean
  compatibilityReviewed: boolean
  readOnly: true
}

export type RemediationResult = {
  recommendations: Remediation[]
  conditions?: InsightCondition[]
}

export type RemediationQuery = {
  provider?: string
  namespace?: string
  resourceKind?: string
  resourceId?: string
  severity?: TimelineSeverity[]
  condition?: string[]
  limit?: number
}

export const storageGuidanceClient = {
  comparison: (query: ComparisonQuery = {}, signal?: AbortSignal) =>
    highlandGet<ProviderComparison>(
      `/storage/comparison${buildInsightQuery({
        provider: query.providers,
        storageClass: query.storageClasses,
        accessMode: query.requiredAccessMode,
        topology: query.requiredTopology,
        snapshot: query.requireSnapshot,
        clone: query.requireClone,
        encryption: query.requireEncryption,
        expansion: query.requireExpansion,
        healthy: query.requireHealthy,
        minimumHeadroom: query.minimumHeadroomPercent,
        minimumSupport: query.minimumSupportLevel,
        limit: query.limit,
      })}`, { signal },
    ),
  remediations: (query: RemediationQuery = {}, signal?: AbortSignal) =>
    highlandGet<RemediationResult>(
      `/storage/remediations${buildInsightQuery({
        provider: query.provider,
        namespace: query.namespace,
        resourceKind: query.resourceKind,
        resourceId: query.resourceId,
        severity: query.severity,
        condition: query.condition,
        limit: query.limit,
      })}`, { signal },
    ),
}

export const storageGuidanceKeys = {
  root: ['storage', 'guidance'] as const,
  comparison: (query: ComparisonQuery) => [...storageGuidanceKeys.root, 'comparison', query] as const,
  remediations: (query: RemediationQuery) => [...storageGuidanceKeys.root, 'remediations', query] as const,
}

export function useProviderComparison(query: ComparisonQuery = {}) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageGuidanceKeys.comparison(query),
    queryFn: ({ signal }) => storageGuidanceClient.comparison(query, signal),
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 120_000 : 60_000,
  })
}

export function useStorageRemediations(query: RemediationQuery = {}) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageGuidanceKeys.remediations(query),
    queryFn: ({ signal }) => storageGuidanceClient.remediations(query, signal),
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 60_000 : 30_000,
  })
}
