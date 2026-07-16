import { Link, useParams, useSearchParams } from 'react-router-dom'
import { AlertTriangle, GitBranch, ShieldCheck } from 'lucide-react'
import {
  useProviderDrift,
  useRelationships,
  useResourceRelationships,
  useStorageImpact,
  type GraphNode,
  type RelationshipConfidence,
} from '@/api/storage/context'
import {
  useCapacityForecast,
  useCapacityOwnership,
  useStorageTimeline,
  type CapacityMeasure,
} from '@/api/storage/insights'
import { useStorageProviders } from '@/api/storage/hooks'
import { useProviderComparison, useStorageRemediations } from '@/api/storage/guidance'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { CapacityOwnershipPanel, TimelinePanel } from './StorageInsightPanels'
import { ProviderComparisonPanel, RemediationGuidancePanel } from './StorageGuidancePanels'

const forecastMeasure: CapacityMeasure = 'pvc-requested'

export function StorageInsightsPage() {
  const [params, setParams] = useSearchParams()
  const provider = params.get('provider') ?? ''
  const providers = useStorageProviders()
  const timeline = useStorageTimeline({ provider: provider || undefined, limit: 100 })
  const capacity = useCapacityOwnership({ provider: provider || undefined, limit: 200 })
  const forecast = useCapacityForecast(provider, { measure: forecastMeasure, horizon: '720h' })
  const comparison = useProviderComparison({ providers: provider ? [provider] : undefined, requireHealthy: true, requireExpansion: true, limit: 100 })
  const remediations = useStorageRemediations({ provider: provider || undefined, limit: 100 })
  return <div data-testid="storage-insights-page">
    <PageHeader title="Storage context" description="Provider-attributed incident history, capacity ownership, and cross-layer evidence without collapsing unlike measurements." />
    <Card className="mb-4">
      <CardContent className="pt-6">
        <label className="grid gap-1 text-sm sm:max-w-md">
          <span className="font-medium">Provider scope</span>
          <select
            aria-label="Provider scope"
            className="h-10 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3"
            value={provider}
            onChange={(event) => {
              const next = new URLSearchParams(params)
              if (event.target.value) next.set('provider', event.target.value); else next.delete('provider')
              setParams(next)
            }}
          >
            <option value="">All authoritatively attributed storage</option>
            {(providers.data?.data ?? []).map((item) => <option key={item.id} value={item.id}>{item.displayName}</option>)}
          </select>
        </label>
      </CardContent>
    </Card>
    <div className="grid gap-4 xl:grid-cols-2">
      <TimelinePanel timeline={timeline.data} isLoading={timeline.isLoading} error={timeline.error as Error | null} />
      <CapacityOwnershipPanel
        ownership={capacity.data}
        forecast={provider ? forecast.data : undefined}
        isLoading={capacity.isLoading}
        error={capacity.error as Error | null}
      />
      <div className="xl:col-span-2"><ProviderComparisonPanel comparison={comparison.data} isLoading={comparison.isLoading} error={comparison.error as Error | null} /></div>
      <div className="xl:col-span-2"><RemediationGuidancePanel result={remediations.data} isLoading={remediations.isLoading} error={remediations.error as Error | null} /></div>
    </div>
  </div>
}

export function ProviderContextPage() {
  const { providerId = '' } = useParams()
  const provider = decodeURIComponent(providerId)
  const graph = useRelationships({ provider, kind: 'provider', depth: 4, limit: 200 })
  const drift = useProviderDrift(provider)
  const timeline = useStorageTimeline({ provider, limit: 50 })
  const capacity = useCapacityOwnership({ provider, limit: 200 })
  const forecast = useCapacityForecast(provider, { measure: forecastMeasure, horizon: '720h' })
  const comparison = useProviderComparison({ providers: [provider], requireHealthy: true, requireExpansion: true, limit: 100 })
  const remediations = useStorageRemediations({ provider, limit: 100 })
  return <div data-testid="provider-context-page">
    <PageHeader title={`${provider} context`} description="Kubernetes-to-backend relationships, desired/runtime drift, incident history, and capacity ownership for this provider." />
    <div className="grid gap-4 xl:grid-cols-2">
      <RelationshipPanel graph={graph.data} isLoading={graph.isLoading} error={graph.error as Error | null} />
      <DriftPanel report={drift.data} isLoading={drift.isLoading} error={drift.error as Error | null} />
      <TimelinePanel timeline={timeline.data} isLoading={timeline.isLoading} error={timeline.error as Error | null} title="Provider timeline" />
      <CapacityOwnershipPanel ownership={capacity.data} forecast={forecast.data} isLoading={capacity.isLoading} error={capacity.error as Error | null} />
      <div className="xl:col-span-2"><ProviderComparisonPanel comparison={comparison.data} isLoading={comparison.isLoading} error={comparison.error as Error | null} /></div>
      <div className="xl:col-span-2"><RemediationGuidancePanel result={remediations.data} isLoading={remediations.isLoading} error={remediations.error as Error | null} /></div>
    </div>
  </div>
}

export function ResourceRelationshipsPage() {
  const { kind = '', resourceId = '' } = useParams()
  const [params] = useSearchParams()
  const provider = params.get('provider') ?? ''
  const id = decodeURIComponent(resourceId)
  const graph = useResourceRelationships(provider, kind, id)
  const impact = useStorageImpact(provider, kind, id)
  return <div data-testid="resource-relationships-page">
    <PageHeader title="Resource relationships" description="Exact, evidence-labelled dependencies and workload impact for one canonical storage identity." />
    {!provider ? <Alert tone="warning"><AlertTitle>Provider is required</AlertTitle><AlertDescription>Open this page from a provider or storage resource so attribution remains explicit.</AlertDescription></Alert> : null}
    <div className="grid gap-4 xl:grid-cols-2">
      <RelationshipPanel graph={graph.data} isLoading={graph.isLoading} error={graph.error as Error | null} />
      <ImpactPanel result={impact.data} isLoading={impact.isLoading} error={impact.error as Error | null} />
    </div>
  </div>
}

export function RelationshipPanel({
  graph,
  isLoading,
  error,
}: {
  graph?: ReturnType<typeof useRelationships>['data']
  isLoading: boolean
  error: Error | null
}) {
  return <Card data-testid="relationship-panel">
    <CardHeader><CardTitle className="flex items-center gap-2"><GitBranch size={18} /> Relationship graph</CardTitle></CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {graph?.incomplete ? <Alert tone="warning"><AlertTitle>Partial relationship evidence</AlertTitle><AlertDescription>One or more sources were unavailable or bounded. Unknown relationships are not promoted to confirmed.</AlertDescription></Alert> : null}
        {graph?.nodes?.length ? <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead><tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-muted-foreground)]"><th className="py-2">Resource</th><th>Kind</th><th>Freshness</th><th>Relationships</th></tr></thead>
            <tbody>{graph.nodes.map((node) => <GraphRow key={node.id} node={node} graph={graph} />)}</tbody>
          </table>
        </div> : <p className="text-sm text-[var(--color-muted-foreground)]">No exact relationships matched this scope.</p>}
        {graph?.nodes && graph?.edges ? <p className="mt-3 text-xs text-[var(--color-muted-foreground)]">{graph.edges.length} evidence-labelled edges · observed {new Date(graph.observedAt).toLocaleString()}</p> : null}
      </QueryState>
    </CardContent>
  </Card>
}

function GraphRow({ node, graph }: { node: GraphNode; graph: NonNullable<ReturnType<typeof useRelationships>['data']> }) {
  const edges = (graph.edges ?? []).filter((edge) => edge.from === node.id || edge.to === node.id)
  const confidence = weakestConfidence(edges.map((edge) => edge.confidence))
  return <tr className="border-b border-[var(--color-border)] last:border-0">
    <td className="py-2 pr-3"><div className="font-medium">{node.namespace ? `${node.namespace}/` : ''}{node.name}</div><div className="max-w-xs truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{node.id}</div></td>
    <td className="pr-3">{node.kind}</td>
    <td className="pr-3"><Badge tone={node.freshness === 'fresh' ? 'success' : node.freshness === 'stale' ? 'warning' : 'default'}>{node.freshness}</Badge></td>
    <td>{edges.length ? <span>{edges.length} · <Badge tone={confidence === 'authoritative' ? 'success' : 'warning'}>{confidence}</Badge></span> : '—'}</td>
  </tr>
}

function DriftPanel({ report, isLoading, error }: {
  report?: ReturnType<typeof useProviderDrift>['data']
  isLoading: boolean
  error: Error | null
}) {
  return <Card data-testid="drift-panel">
    <CardHeader><CardTitle className="flex items-center justify-between gap-3"><span className="flex items-center gap-2"><AlertTriangle size={18} /> Desired/runtime drift</span>{report ? <Badge tone={report.summary.error ? 'danger' : report.summary.warning ? 'warning' : 'success'}>{report.summary.total} active</Badge> : null}</CardTitle></CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {report?.incomplete ? <Alert tone="warning"><AlertTitle>Drift evidence is partial</AlertTitle><AlertDescription>Runtime or desired-state evidence is unavailable; Highland will not recommend destructive action from incomplete data.</AlertDescription></Alert> : null}
        {report?.data.length ? <ul className="space-y-2">{report.data.map((item) => <li key={item.id} className="rounded-md border border-[var(--color-border)] p-3 text-sm"><div className="flex flex-wrap items-center justify-between gap-2"><span className="font-medium">{item.category}</span><Badge tone={item.severity === 'error' ? 'danger' : item.severity === 'warning' ? 'warning' : 'default'}>{item.severity}</Badge></div><p className="mt-1">{item.message}</p><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{item.duration} · action surface: {item.actionSurface}{item.actionable ? '' : ' · informational only'}</p></li>)}</ul> : <p className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]"><ShieldCheck size={16} /> No supported desired/runtime drift is active.</p>}
      </QueryState>
    </CardContent>
  </Card>
}

function ImpactPanel({ result, isLoading, error }: {
  result?: ReturnType<typeof useStorageImpact>['data']
  isLoading: boolean
  error: Error | null
}) {
  const summary = result?.summary
  const confirmed = result?.confirmed ?? []
  const potential = result?.potential ?? []
  const unknown = result?.unknown ?? []
  const hasImpactData = Boolean(summary || confirmed.length || potential.length || unknown.length)

  return <Card data-testid="impact-panel">
    <CardHeader><CardTitle>Workload impact</CardTitle></CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {result?.incomplete ? <Alert tone="warning"><AlertTitle>Impact is incomplete</AlertTitle><AlertDescription>Required evidence is unavailable. Destructive plans must fail closed.</AlertDescription></Alert> : null}
        {hasImpactData ? <>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <ImpactFact label="Workloads" value={summary?.workloadCount ?? 0} />
            <ImpactFact label="Pods" value={summary?.podCount ?? 0} />
            <ImpactFact label="Namespaces" value={summary?.namespaceCount ?? 0} />
            <ImpactFact label="Snapshots" value={summary?.snapshotCount ?? 0} />
          </div>
          <ImpactList title="Confirmed" items={confirmed} tone="success" />
          <ImpactList title="Potential" items={potential} tone="warning" />
          <ImpactList title="Unknown" items={unknown} tone="default" />
        </> : !isLoading && !error ? <p className="text-sm text-[var(--color-muted-foreground)]">No workload impact evidence matched this resource.</p> : null}
      </QueryState>
    </CardContent>
  </Card>
}

function ImpactFact({ label, value }: { label: string; value: number }) {
  return <div className="rounded-md bg-[var(--color-muted)] p-2"><div className="text-xs text-[var(--color-muted-foreground)]">{label}</div><div className="text-lg font-semibold">{value}</div></div>
}

function ImpactList({ title, items, tone }: {
  title: string
  items: NonNullable<ReturnType<typeof useStorageImpact>['data']>['confirmed']
  tone: 'success' | 'warning' | 'default'
}) {
  if (!items.length) return null
  return <section className="mt-4"><h4 className="mb-2 flex items-center gap-2 text-sm font-semibold">{title}<Badge tone={tone}>{items.length}</Badge></h4><ul className="space-y-1 text-sm">{items.slice(0, 12).map((item) => <li key={`${title}:${item.node.id}`} className="rounded border border-[var(--color-border)] px-2 py-1">{item.node.kind} {item.node.namespace ? `${item.node.namespace}/` : ''}{item.node.name}</li>)}</ul></section>
}

function weakestConfidence(values: RelationshipConfidence[]): RelationshipConfidence {
  const order: RelationshipConfidence[] = ['unknown', 'potential', 'derived', 'authoritative']
  return values.reduce((weakest, value) => order.indexOf(value) < order.indexOf(weakest) ? value : weakest, 'authoritative')
}

export function ResourceContextLink({ provider, kind, id }: { provider: string; kind: string; id: string }) {
  return <Link className="text-sm font-medium text-[var(--color-primary)] hover:underline" to={`/storage/relationships/${encodeURIComponent(kind)}/${encodeURIComponent(id)}?provider=${encodeURIComponent(provider)}`}>View relationships and impact</Link>
}
