import { Link, useParams, useSearchParams } from 'react-router-dom'
import { AlertTriangle, Boxes, GitBranch, Layers3, Server, ShieldCheck, Users } from 'lucide-react'
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
import { useStorageProvider, useStorageProviders } from '@/api/storage/hooks'
import type { ProviderDescriptor, StorageCondition } from '@/api/storage/types'
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
  const descriptor = useStorageProvider(provider)
  const providers = useStorageProviders()
  const graph = useRelationships({ provider, kind: 'provider', depth: 4, limit: 200 })
  const drift = useProviderDrift(provider)
  const timeline = useStorageTimeline({ provider, limit: 50 })
  const capacity = useCapacityOwnership({ provider, limit: 200 })
  const forecast = useCapacityForecast(provider, { measure: forecastMeasure, horizon: '720h' })
  const comparison = useProviderComparison({ providers: [provider], requireHealthy: true, requireExpansion: true, limit: 100 })
  const remediations = useStorageRemediations({ provider, limit: 100 })
  const providerInfo = descriptor.data
  const providerList = providers.data?.data ?? []
  const displayName = providerInfo?.displayName || provider
  return <div data-testid="provider-context-page">
    <PageHeader
      title={`${displayName} context`}
      description={`A provider-specific view of ${displayName}'s storage surface, Kubernetes bindings, consumers, health evidence, and operational guidance.`}
      actions={<>
        {providerInfo?.version ? <Badge>{providerInfo.version}</Badge> : null}
        {providerInfo?.supportLevel ? <Badge tone="info">{providerInfo.supportLevel}</Badge> : null}
        {providerInfo?.health ? <Badge tone={healthTone(providerInfo.health.status)}>{providerInfo.health.status}</Badge> : null}
      </>}
    />
    <ProviderContextSummary
      provider={providerInfo}
      graph={graph.data}
      drift={drift.data}
      providers={providerList}
      isLoading={descriptor.isLoading || graph.isLoading}
    />
    <ProviderEvidenceGaps graph={graph.data} drift={drift.data} />
    <div className="mt-4 grid gap-4 xl:grid-cols-2">
      <div className="xl:col-span-2">
        <RelationshipPanel
          graph={graph.data}
          isLoading={graph.isLoading}
          error={graph.error as Error | null}
          provider={providerInfo}
          providers={providerList}
          workspace
          showPartialAlert={false}
        />
      </div>
      <DriftPanel report={drift.data} isLoading={drift.isLoading} error={drift.error as Error | null} showPartialAlert={false} />
      <TimelinePanel timeline={timeline.data} isLoading={timeline.isLoading} error={timeline.error as Error | null} title="Provider timeline" />
      <div className="xl:col-span-2"><CapacityOwnershipPanel ownership={capacity.data} forecast={forecast.data} isLoading={capacity.isLoading} error={capacity.error as Error | null} /></div>
      <div className="xl:col-span-2"><ProviderComparisonPanel comparison={comparison.data} isLoading={comparison.isLoading} error={comparison.error as Error | null} compact /></div>
      <div className="xl:col-span-2"><RemediationGuidancePanel result={remediations.data} isLoading={remediations.isLoading} error={remediations.error as Error | null} /></div>
    </div>
  </div>
}

function ProviderContextSummary({
  provider,
  graph,
  drift,
  providers,
  isLoading,
}: {
  provider?: ProviderDescriptor
  graph?: ReturnType<typeof useRelationships>['data']
  drift?: ReturnType<typeof useProviderDrift>['data']
  providers: ProviderDescriptor[]
  isLoading: boolean
}) {
  const nodes = graph?.nodes ?? []
  const consumers = nodes.filter((node) => consumerKinds.has(node.kind))
  const externalConsumers = consumers.filter((node) => Boolean(crossProvider(node, provider?.id, providers)))
  const backendResources = nodes.filter((node) => nodeRole(node) === 'provider')
  const evidenceComplete = !graph?.incomplete && !drift?.incomplete
  return <Card data-testid="provider-context-summary">
    <CardContent className="grid gap-4 pt-4 sm:grid-cols-2 xl:grid-cols-5">
      <ContextFact icon={Server} label="Provider" value={provider?.displayName || (isLoading ? 'Loading…' : 'Unavailable')} detail={provider?.kind || 'CSI provider'} />
      <ContextFact icon={Boxes} label="Provider surface" value={backendResources.length} detail="driver, classes, and backend objects" />
      <ContextFact icon={Users} label="Consumers" value={consumers.length} detail={externalConsumers.length ? `${externalConsumers.length} cross-provider` : 'within workload namespaces'} />
      <ContextFact icon={AlertTriangle} label="Active drift" value={drift?.summary.total ?? 0} detail={drift?.summary.error ? `${drift.summary.error} errors` : 'no critical drift'} />
      <ContextFact icon={ShieldCheck} label="Evidence" value={evidenceComplete ? 'Complete' : 'Partial'} detail={graph?.observedAt ? `observed ${formatContextTime(graph.observedAt)}` : 'collecting evidence'} tone={evidenceComplete ? 'success' : 'warning'} />
    </CardContent>
  </Card>
}

function ContextFact({
  icon: Icon,
  label,
  value,
  detail,
  tone,
}: {
  icon: typeof Server
  label: string
  value: string | number
  detail: string
  tone?: 'success' | 'warning'
}) {
  return <div className="min-w-0 rounded-md bg-[var(--color-muted)]/45 p-3">
    <div className="flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)]"><Icon size={15} /> {label}</div>
    <div className={`mt-1 text-xl font-semibold ${tone === 'success' ? 'text-emerald-700 dark:text-emerald-300' : tone === 'warning' ? 'text-amber-800 dark:text-amber-300' : ''}`}>{value}</div>
    <div className="mt-0.5 truncate text-xs text-[var(--color-muted-foreground)]" title={detail}>{detail}</div>
  </div>
}

function ProviderEvidenceGaps({
  graph,
  drift,
}: {
  graph?: ReturnType<typeof useRelationships>['data']
  drift?: ReturnType<typeof useProviderDrift>['data']
}) {
  const conditions = [
    ...(graph?.conditions ?? []),
    ...(graph?.nodes.flatMap((node) => node.conditions ?? []) ?? []),
    ...(drift?.conditions ?? []),
  ]
  const reasons = Array.from(new Set(
    conditions
      .filter((condition) => condition.severity !== 'ok' && (condition.status === 'False' || condition.status === 'Unknown' || condition.severity === 'warning' || condition.severity === 'error'))
      .map(conditionMessage)
      .filter(Boolean),
  ))
  if (!graph?.incomplete && !drift?.incomplete && reasons.length === 0) return null
  return <Alert tone="warning" className="mt-4" data-testid="provider-evidence-gaps">
    <AlertTitle>Evidence gaps</AlertTitle>
    <AlertDescription>
      <span>Highland keeps uncertain relationships informational and blocks destructive guidance that depends on missing evidence.</span>
      {reasons.length ? <ul className="mt-2 list-disc space-y-1 pl-5">{reasons.slice(0, 5).map((reason) => <li key={reason}>{reason}</li>)}</ul> : null}
    </AlertDescription>
  </Alert>
}

function conditionMessage(condition: StorageCondition) {
  return condition.message || condition.reason || condition.type
}

function healthTone(status: ProviderDescriptor['health']['status']): 'success' | 'warning' | 'danger' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'warning' || status === 'info') return 'warning'
  if (status === 'error') return 'danger'
  return 'default'
}

function formatContextTime(value: string) {
  return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
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
  provider,
  providers = [],
  workspace = false,
  showPartialAlert = true,
}: {
  graph?: ReturnType<typeof useRelationships>['data']
  isLoading: boolean
  error: Error | null
  provider?: ProviderDescriptor
  providers?: ProviderDescriptor[]
  workspace?: boolean
  showPartialAlert?: boolean
}) {
  const groups = workspace && graph ? relationshipGroups(graph.nodes) : []
  return <Card data-testid="relationship-panel">
    <CardHeader>
      <CardTitle className="flex items-center gap-2"><GitBranch size={18} /> Storage relationships</CardTitle>
      {workspace ? <p className="text-xs text-[var(--color-muted-foreground)]">The selected provider stays central. Kubernetes bindings and consuming workloads are separated so they are not mistaken for provider-owned resources.</p> : null}
    </CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {showPartialAlert && graph?.incomplete ? <Alert tone="warning"><AlertTitle>Partial relationship evidence</AlertTitle><AlertDescription>One or more sources were unavailable or bounded. Unknown relationships are not promoted to confirmed.</AlertDescription></Alert> : null}
        {workspace && graph?.nodes?.length ? <div className="space-y-5">
          {groups.map((group) => <RelationshipGroup
            key={group.role}
            group={group}
            graph={graph}
            provider={provider}
            providers={providers}
          />)}
        </div> : graph?.nodes?.length ? <div className="overflow-x-auto">
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

const consumerKinds = new Set(['pvc', 'pod', 'workload'])
const kubernetesBindingKinds = new Set(['pv', 'volumeattachment', 'snapshot', 'volumesnapshot'])

type RelationshipRole = 'provider' | 'binding' | 'consumer'

function nodeRole(node: GraphNode): RelationshipRole {
  if (consumerKinds.has(node.kind)) return 'consumer'
  if (kubernetesBindingKinds.has(node.kind)) return 'binding'
  return 'provider'
}

function relationshipGroups(nodes: GraphNode[]) {
  const definitions: Array<{ role: RelationshipRole; title: string; description: string; icon: typeof Server }> = [
    { role: 'provider', title: 'Provider surface', description: 'CSI driver, StorageClasses, and backend resources owned or served by this provider.', icon: Server },
    { role: 'binding', title: 'Kubernetes storage bindings', description: 'Portable Kubernetes objects that connect claims to the selected provider.', icon: Layers3 },
    { role: 'consumer', title: 'Workloads consuming this provider', description: 'Claims and workloads backed by this provider, including explicit cross-provider dependencies.', icon: Users },
  ]
  return definitions
    .map((definition) => ({ ...definition, nodes: nodes.filter((node) => nodeRole(node) === definition.role) }))
    .filter((group) => group.nodes.length > 0)
}

function RelationshipGroup({
  group,
  graph,
  provider,
  providers,
}: {
  group: ReturnType<typeof relationshipGroups>[number]
  graph: NonNullable<ReturnType<typeof useRelationships>['data']>
  provider?: ProviderDescriptor
  providers: ProviderDescriptor[]
}) {
  const Icon = group.icon
  return <section aria-labelledby={`relationship-${group.role}`}>
    <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h4 id={`relationship-${group.role}`} className="flex items-center gap-2 text-sm font-semibold"><Icon size={16} /> {group.title}</h4>
        <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">{group.description}</p>
      </div>
      <Badge>{group.nodes.length}</Badge>
    </div>
    <div className="overflow-x-auto rounded-md border border-[var(--color-border)]">
      <table className="w-full text-left text-sm">
        <thead className="bg-[var(--color-muted)]/40 text-xs text-[var(--color-muted-foreground)]">
          <tr><th className="px-3 py-2 font-medium">Resource</th><th className="px-3 py-2 font-medium">Role</th><th className="px-3 py-2 font-medium">Evidence</th><th className="px-3 py-2 font-medium">Connections</th></tr>
        </thead>
        <tbody>{group.nodes.map((node) => <WorkspaceGraphRow key={node.id} node={node} graph={graph} provider={provider} providers={providers} />)}</tbody>
      </table>
    </div>
  </section>
}

function WorkspaceGraphRow({
  node,
  graph,
  provider,
  providers,
}: {
  node: GraphNode
  graph: NonNullable<ReturnType<typeof useRelationships>['data']>
  provider?: ProviderDescriptor
  providers: ProviderDescriptor[]
}) {
  const edges = (graph.edges ?? []).filter((edge) => edge.from === node.id || edge.to === node.id)
  const confidence = weakestConfidence(edges.map((edge) => edge.confidence))
  const relationTypes = Array.from(new Set(edges.map((edge) => edge.type.replaceAll('-', ' '))))
  const otherProvider = crossProvider(node, provider?.id, providers)
  const role = nodeRole(node) === 'consumer'
    ? otherProvider
      ? `Cross-provider consumer · ${otherProvider.displayName}`
      : node.namespace
        ? `Consumer · ${node.namespace}`
        : 'Consumer'
    : nodeRole(node) === 'binding'
      ? 'Kubernetes binding'
      : node.kind === 'storageclass' || node.kind === 'csidriver'
        ? 'CSI surface'
        : 'Provider owned'
  return <tr className="border-t border-[var(--color-border)] first:border-t-0">
    <td className="px-3 py-3 pr-5">
      <div className="font-medium">{node.namespace ? `${node.namespace}/` : ''}{node.name}</div>
      <div className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">{resourceKindLabel(node.kind)}</div>
      <details className="mt-1 text-[10px] text-[var(--color-muted-foreground)]">
        <summary className="cursor-pointer select-none">Canonical identity</summary>
        <span className="break-all font-mono">{node.id}</span>
      </details>
    </td>
    <td className="px-3 py-3 pr-5"><Badge tone={otherProvider ? 'info' : 'default'}>{role}</Badge></td>
    <td className="px-3 py-3 pr-5"><div className="flex flex-wrap gap-1"><Badge tone={node.freshness === 'fresh' ? 'success' : node.freshness === 'stale' ? 'warning' : 'default'}>{node.freshness}</Badge>{edges.length ? <Badge tone={confidence === 'authoritative' ? 'success' : 'warning'}>{confidence}</Badge> : null}</div></td>
    <td className="max-w-sm px-3 py-3 text-xs text-[var(--color-muted-foreground)]">{edges.length ? `${edges.length} · ${relationTypes.join(', ')}` : 'No observed connection'}</td>
  </tr>
}

function crossProvider(node: GraphNode, selectedProviderId: string | undefined, providers: ProviderDescriptor[]) {
  if (!node.namespace) return undefined
  return providers.find((candidate) =>
    candidate.id !== selectedProviderId
    && Boolean(candidate.namespace)
    && candidate.namespace === node.namespace,
  )
}

function resourceKindLabel(kind: string) {
  const labels: Record<string, string> = {
    provider: 'Provider',
    csidriver: 'CSI driver',
    storageclass: 'StorageClass',
    pv: 'PersistentVolume',
    pvc: 'PersistentVolumeClaim',
    pod: 'Pod',
    workload: 'Workload',
    volumeattachment: 'VolumeAttachment',
    'longhorn-volume': 'Longhorn volume',
    'ceph-rbd-image': 'Ceph RBD image',
    'ceph-block-pool': 'Ceph block pool',
    'ceph-filesystem': 'Ceph filesystem',
  }
  return labels[kind] || kind.replaceAll('-', ' ')
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

function DriftPanel({ report, isLoading, error, showPartialAlert = true }: {
  report?: ReturnType<typeof useProviderDrift>['data']
  isLoading: boolean
  error: Error | null
  showPartialAlert?: boolean
}) {
  return <Card data-testid="drift-panel">
    <CardHeader><CardTitle className="flex items-center justify-between gap-3"><span className="flex items-center gap-2"><AlertTriangle size={18} /> Desired/runtime drift</span>{report ? <Badge tone={report.summary.error ? 'danger' : report.summary.warning ? 'warning' : 'success'}>{report.summary.total} active</Badge> : null}</CardTitle></CardHeader>
    <CardContent>
      <QueryState isLoading={isLoading} error={error}>
        {showPartialAlert && report?.incomplete ? <Alert tone="warning"><AlertTitle>Drift evidence is partial</AlertTitle><AlertDescription>Runtime or desired-state evidence is unavailable; Highland will not recommend destructive action from incomplete data.</AlertDescription></Alert> : null}
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
