import { useMemo } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Database,
  Gauge,
  GitBranch,
  HardDrive,
  Layers3,
  RefreshCw,
  Server,
  ShieldCheck,
} from 'lucide-react'
import { canonicalGraphId } from '@/api/storage/context'
import { useProviderResource, useProviderResources, useProviderSummary } from '@/api/storage/hooks'
import type { ProviderDescriptor, StorageCondition, StorageFilters } from '@/api/storage/types'
import { DataTable } from '@/components/data/DataTable'
import { Donut, LegendRow } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { ResourceContextLink } from './StorageContextPages'
import { ProviderWorkloadFootprint } from './ProviderWorkloadFootprint'

type OpenEBSEngine = {
  id: string
  name: string
  driver: string
  mode: 'local' | 'replicated'
  installed: boolean
  description: string
  resourceKinds?: string[]
  servedResources?: string[]
}

type OpenEBSComponent = {
  id: string
  name: string
  kind: string
  engine: string
  desired: number
  readyReplicas: number
  ready: boolean
  images?: string[]
}

type OpenEBSSummary = {
  providerId: string
  providerKind: string
  namespace: string
  version?: string
  health: ProviderDescriptor['health']
  engines: OpenEBSEngine[]
  components: OpenEBSComponent[]
  resourceCounts: Record<string, number>
  conditions?: StorageCondition[]
  observedAt: string
}

type OpenEBSResource = Record<string, unknown> & {
  id?: string
  name?: string
  namespace?: string
  engine?: string
  state?: string
  phase?: string
  health?: string
  node?: string
  capacity?: string | number
  used?: string | number
  available?: string | number
  pool?: string
  volumeGroup?: string
  volumeHandle?: string
  filesystem?: string
  source?: string
  observedAt?: string
}

const resourceConfig: Record<string, {
  title: string
  description: string
  guidance: string
  mode: 'local' | 'replicated' | 'control-plane'
}> = {
  components: {
    title: 'OpenEBS components',
    description: 'Controller and node workload rollout health across installed OpenEBS engines.',
    guidance: 'Every expected Deployment and DaemonSet should have all desired replicas ready.',
    mode: 'control-plane',
  },
  'disk-pools': {
    title: 'Mayastor disk pools',
    description: 'Block devices assigned to Replicated PV Mayastor storage nodes.',
    guidance: 'Every configured DiskPool should be online. Production replicated volumes need pools on independent nodes and failure domains.',
    mode: 'replicated',
  },
  'lvm-nodes': {
    title: 'LVM nodes and volume groups',
    description: 'Node-local LVM capacity advertised to the OpenEBS LocalPV LVM controller.',
    guidance: 'Nodes should be ready and each selected volume group should retain enough free extents for workload growth.',
    mode: 'local',
  },
  'lvm-volumes': {
    title: 'LVM volumes',
    description: 'Node-local logical volumes created for Kubernetes PersistentVolumes.',
    guidance: 'Volumes should be ready on healthy nodes. LocalPV LVM does not survive loss of the node that owns the volume.',
    mode: 'local',
  },
  'lvm-snapshots': {
    title: 'LVM snapshots',
    description: 'LocalPV LVM snapshot records and their source-volume state.',
    guidance: 'Snapshots should be ready and remain on a healthy node and snapshot-capable thin pool.',
    mode: 'local',
  },
  'zfs-nodes': {
    title: 'ZFS nodes and pools',
    description: 'Node-local ZFS pool capacity advertised to the OpenEBS ZFS controller.',
    guidance: 'Pools should be online with enough free capacity and no underlying ZFS device faults.',
    mode: 'local',
  },
  'zfs-volumes': {
    title: 'ZFS volumes',
    description: 'ZFS datasets and zvols backing Kubernetes PersistentVolumes.',
    guidance: 'Volumes should be ready on healthy nodes. Verify compression, filesystem, and pool placement match workload policy.',
    mode: 'local',
  },
  'zfs-snapshots': {
    title: 'ZFS snapshots',
    description: 'Point-in-time ZFS snapshots managed through LocalPV ZFS.',
    guidance: 'Snapshots should be ready and their source datasets and ZFS pools must remain available.',
    mode: 'local',
  },
  'zfs-backups': {
    title: 'ZFS backups',
    description: 'OpenEBS ZFS backup records and transfer status.',
    guidance: 'A backup is useful only after successful completion and a tested restore path.',
    mode: 'local',
  },
  'zfs-restores': {
    title: 'ZFS restores',
    description: 'OpenEBS ZFS restore records and destination state.',
    guidance: 'Restores should complete onto an explicitly selected healthy pool before applications consume the result.',
    mode: 'local',
  },
  'hostpath-volumes': {
    title: 'HostPath local volumes',
    description: 'Non-replicated node-local directories provisioned by OpenEBS Dynamic LocalPV.',
    guidance: 'The owning node must remain healthy. These volumes do not provide replication or failover when that node is lost.',
    mode: 'local',
  },
}

function severityTone(status: string): 'success' | 'warning' | 'danger' | 'default' {
  if (status === 'ok' || status === 'ready' || status === 'online' || status === 'bound') return 'success'
  if (status === 'error' || status === 'faulted' || status === 'offline' || status === 'failed') return 'danger'
  if (status === 'warning' || status === 'degraded' || status === 'pending') return 'warning'
  return 'default'
}

function stateValue(resource: OpenEBSResource) {
  return String(resource.health ?? resource.state ?? resource.phase ?? 'Unknown')
}

function formatObserved(value: unknown) {
  if (!value) return 'Unknown'
  const date = new Date(String(value))
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString()
}

function sourceLabel(value: unknown) {
  const source = String(value ?? '')
  const labels: Record<string, string> = {
    'openebs-crd': 'OpenEBS CRD',
    'kubernetes-pv': 'Kubernetes PV',
    'kubernetes-workload': 'Kubernetes workload',
    'kubernetes-discovery': 'Kubernetes discovery',
  }
  return labels[source] ?? (source.replaceAll('-', ' ') || 'Unknown')
}

function PartialConditions({ conditions }: { conditions?: StorageCondition[] }) {
  if (!conditions?.length) return null
  return <div className="space-y-2">
    {conditions.map((condition) => <Alert key={`${condition.type}:${condition.reason}`} tone={condition.severity === 'error' ? 'danger' : 'warning'}>
      <AlertTitle>{condition.type}</AlertTitle>
      <AlertDescription>{condition.message ?? condition.reason ?? 'This OpenEBS source is partially available.'}</AlertDescription>
    </Alert>)}
  </div>
}

export function OpenEBSProviderPage({ provider }: { provider: ProviderDescriptor }) {
  const summary = useProviderSummary<OpenEBSSummary>(provider.id)
  const data = summary.data
  const installed = data?.engines.filter((engine) => engine.installed) ?? []
  const local = installed.filter((engine) => engine.mode === 'local')
  const replicated = installed.filter((engine) => engine.mode === 'replicated')
  const available = data?.engines.filter((engine) => !engine.installed) ?? []
  const readyComponents = data?.components.filter((component) => component.ready).length ?? 0
  const totalResources = Object.values(data?.resourceCounts ?? {}).reduce((sum, count) => sum + count, 0)
  const healthy = provider.health.status === 'ok'

  return <div data-testid="openebs-provider-page">
    <PageHeader
      title="Dashboard"
      description="Engine readiness, resilience posture, and provider-scoped Kubernetes ownership."
      actions={<Button type="button" variant="outline" onClick={() => void summary.refetch()}><RefreshCw size={15} /> Refresh</Button>}
    />
    <QueryState isLoading={summary.isLoading} error={summary.error as Error | null} onRetry={() => void summary.refetch()}>
      {data ? <div className="space-y-5">
        <section className={`rounded-xl border p-5 ${healthy ? 'border-[var(--color-success)]/30 bg-[var(--color-success)]/5' : 'border-[var(--color-warning)]/40 bg-[var(--color-warning)]/5'}`}>
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex items-start gap-3">
              <div className={`rounded-full p-2.5 ${healthy ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]' : 'bg-[var(--color-warning)]/10 text-[var(--color-warning)]'}`}>
                {healthy ? <CheckCircle2 size={22} /> : <AlertTriangle size={22} />}
              </div>
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-xl font-semibold">{healthy ? 'OpenEBS is healthy' : 'OpenEBS needs attention'}</h2>
                  <Badge tone={healthy ? 'success' : 'warning'}>{provider.health.status}</Badge>
                  <Badge tone="info">read only</Badge>
                </div>
                <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
                  {installed.length} installed engine{installed.length === 1 ? '' : 's'} · {readyComponents}/{data.components.length} components ready
                  {data.version ? ` · version ${data.version}` : ''}
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Link className="inline-flex h-9 items-center gap-2 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-[var(--color-primary-foreground)]" to={`/storage/providers/${encodeURIComponent(provider.id)}/context`}><GitBranch size={15} /> Context & impact</Link>
              <Link className="inline-flex h-9 items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-4 text-sm font-medium" to={`/storage/classes?provider=${encodeURIComponent(provider.id)}`}><Layers3 size={15} /> Storage classes</Link>
            </div>
          </div>
        </section>

        <PartialConditions conditions={data.conditions} />

        <section aria-labelledby="openebs-operational-signals-heading">
          <div className="mb-3"><h2 id="openebs-operational-signals-heading" className="text-base font-semibold">Operational signals</h2><p className="text-sm text-[var(--color-muted-foreground)]">Engine availability, controller readiness, observed resources, and provisioning model.</p></div>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <SummaryCard icon={Gauge} label="Installed engines" value={String(installed.length)} detail={`${replicated.length} replicated · ${local.length} local`} />
            <SummaryCard icon={Server} label="Component readiness" value={`${readyComponents}/${data.components.length}`} detail="Deployments and DaemonSets ready" warning={readyComponents !== data.components.length} />
            <SummaryCard icon={Database} label="Provider resources" value={totalResources.toLocaleString()} detail="Provider-native records observed" />
            <SummaryCard icon={ShieldCheck} label="Resilience model" value={replicated.length ? 'Replicated' : local.length ? 'Node-local' : 'Unavailable'} detail={replicated.length ? 'At least one replicated engine is installed' : 'No replicated engine is installed'} warning={!replicated.length} />
          </div>
        </section>

        <section aria-labelledby="openebs-capacity-resilience-heading">
          <div className="mb-3"><h2 id="openebs-capacity-resilience-heading" className="text-base font-semibold">Capacity & resilience</h2><p className="text-sm text-[var(--color-muted-foreground)]">OpenEBS engine capability and the failure behavior it implies for workloads.</p></div>
          <Card>
            <CardHeader><CardTitle>Engine resilience posture</CardTitle></CardHeader>
            <CardContent className="flex flex-col gap-5 sm:flex-row sm:items-center">
              <Donut slices={[
                { label: 'Replicated engines', value: replicated.length, color: 'var(--color-success)' },
                { label: 'Local engines', value: local.length, color: 'var(--color-warning)' },
              ]} />
              <div className="min-w-0 flex-1 space-y-2">
                <LegendRow color="var(--color-success)" label="Replicated" value={replicated.length} />
                <LegendRow color="var(--color-warning)" label="Node-local" value={local.length} />
                <p className="pt-2 text-xs text-[var(--color-muted-foreground)]">This describes installed engine capability, not the replication state of individual volumes.</p>
                {local.length && !replicated.length ? <p className="rounded-md bg-[var(--color-warning)]/8 p-2 text-xs text-[var(--color-warning)]">Only node-local storage is installed. A node loss can make its volumes unavailable.</p> : null}
              </div>
            </CardContent>
          </Card>
        </section>

        <ProviderWorkloadFootprint provider={provider.id} />

        <section aria-labelledby="openebs-provider-resources-heading">
          <div className="mb-3"><h2 id="openebs-provider-resources-heading" className="text-base font-semibold">Provider resources</h2><p className="text-sm text-[var(--color-muted-foreground)]">Installed storage engines and the control-plane components that operate them.</p></div>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {installed.map((engine) => <EngineCard key={engine.id} provider={provider.id} engine={engine} counts={data.resourceCounts} />)}
          </div>
          {available.length ? <details className="mt-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
            <summary className="cursor-pointer px-4 py-3 text-sm font-medium">{available.length} other supported engine{available.length === 1 ? '' : 's'} not installed</summary>
            <div className="grid gap-3 border-t border-[var(--color-border)] p-4 md:grid-cols-2 xl:grid-cols-3">
              {available.map((engine) => <EngineCard key={engine.id} provider={provider.id} engine={engine} counts={data.resourceCounts} />)}
            </div>
          </details> : null}

        <Card className="mt-4">
          <CardHeader><CardTitle>Control-plane components</CardTitle></CardHeader>
          <CardContent>
            {data.components.length ? <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead><tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-muted-foreground)]"><th className="py-2">Component</th><th>Engine</th><th>Kind</th><th>Readiness</th><th>Image</th></tr></thead>
                <tbody>{data.components.map((component) => <tr key={component.id} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="py-3 pr-3 font-medium">{component.name}</td>
                  <td className="pr-3 capitalize">{component.engine}</td>
                  <td className="pr-3">{component.kind}</td>
                  <td className="pr-3"><Badge tone={component.ready ? 'success' : 'danger'}>{component.readyReplicas}/{component.desired} ready</Badge></td>
                  <td className="max-w-sm truncate text-xs text-[var(--color-muted-foreground)]">{component.images?.join(', ') || '—'}</td>
                </tr>)}</tbody>
              </table>
            </div> : <p className="text-sm text-[var(--color-muted-foreground)]">No OpenEBS controller workloads were observed in {data.namespace}.</p>}
          </CardContent>
        </Card>
        </section>

        {provider.health.conditions.length ? <Card>
          <CardHeader><CardTitle>Health evidence</CardTitle></CardHeader>
          <CardContent className="space-y-2">{provider.health.conditions.map((condition) => <div key={`${condition.type}:${condition.reason}`} className="flex flex-col gap-1 rounded-md border border-[var(--color-border)] p-3 sm:flex-row sm:items-start sm:justify-between">
            <div><div className="font-medium">{condition.type}</div><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{condition.message}</p></div>
            <Badge tone={severityTone(condition.severity)}>{condition.status}</Badge>
          </div>)}</CardContent>
        </Card> : null}
      </div> : null}
    </QueryState>
  </div>
}

function EngineCard({ provider, engine, counts }: { provider: string; engine: OpenEBSEngine; counts: Record<string, number> }) {
  const primaryKind = engine.resourceKinds?.find((kind) => counts[kind] !== undefined) ?? engine.resourceKinds?.[0]
  const count = (engine.resourceKinds ?? []).reduce((sum, kind) => sum + (counts[kind] ?? 0), 0)
  const content = <Card className={`h-full ${engine.installed ? '' : 'opacity-65'}`}>
    <CardHeader>
      <CardTitle className="flex items-center justify-between gap-3">
        <span>{engine.name}</span>
        <Badge tone={engine.installed ? (engine.mode === 'replicated' ? 'success' : 'info') : 'default'}>{engine.installed ? engine.mode : 'not installed'}</Badge>
      </CardTitle>
    </CardHeader>
    <CardContent className="space-y-3 text-sm">
      <p className="text-[var(--color-muted-foreground)]">{engine.description}</p>
      <div className="flex justify-between gap-4"><span>Driver</span><span className="break-all text-right font-mono text-xs">{engine.driver}</span></div>
      <div className="flex justify-between gap-4"><span>Resources</span><span className="font-medium">{count}</span></div>
      {engine.mode === 'local' && engine.installed ? <p className="flex items-start gap-2 rounded-md bg-[var(--color-warning)]/8 p-2 text-xs"><AlertTriangle className="mt-0.5 shrink-0 text-[var(--color-warning)]" size={14} /> Node-local data does not fail over when its owning node is lost.</p> : null}
    </CardContent>
  </Card>
  return engine.installed && primaryKind
    ? <Link className="rounded-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2" to={`/storage/providers/${encodeURIComponent(provider)}/openebs/${primaryKind}`}>{content}</Link>
    : content
}

function SummaryCard({ icon: Icon, label, value, detail, warning }: { icon: typeof Gauge; label: string; value: string; detail: string; warning?: boolean }) {
  return <Card><CardContent className="pt-5">
    <div className="flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)]"><Icon size={15} /> {label}</div>
    <div className={`mt-2 text-2xl font-semibold ${warning ? 'text-[var(--color-warning)]' : ''}`}>{value}</div>
    <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{detail}</p>
  </CardContent></Card>
}

function useResourceFilters() {
  const [params, setParams] = useSearchParams()
  const filters: StorageFilters = { search: params.get('search') || undefined, limit: 100 }
  const setSearch = (value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set('search', value); else next.delete('search')
    setParams(next)
  }
  return { filters, setSearch }
}

function resourceColumns(kind: string, provider: string): ColumnDef<OpenEBSResource, unknown>[] {
  const name: ColumnDef<OpenEBSResource, unknown> = {
    id: 'name', header: 'Resource', cell: ({ row }) => {
      const id = String(row.original.id ?? row.original.name ?? '')
      return <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(provider)}/openebs/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`}>{String(row.original.name ?? id)}</Link>
    },
  }
  const health: ColumnDef<OpenEBSResource, unknown> = { id: 'health', header: 'Health', cell: ({ row }) => <Badge tone={severityTone(stateValue(row.original).toLowerCase())}>{stateValue(row.original)}</Badge> }
  const node: ColumnDef<OpenEBSResource, unknown> = { id: 'node', header: 'Node', accessorFn: (row) => String(row.node ?? '—') }
  const observed: ColumnDef<OpenEBSResource, unknown> = { id: 'observed', header: 'Observed', accessorFn: (row) => formatObserved(row.observedAt) }

  if (kind === 'components') return [
    name,
    { id: 'engine', header: 'Engine', accessorFn: (row) => String(row.engine ?? 'shared') },
    { id: 'kind', header: 'Kind', accessorFn: (row) => String(row.kind ?? '—') },
    { id: 'ready', header: 'Readiness', cell: ({ row }) => <Badge tone={row.original.ready ? 'success' : 'danger'}>{String(row.original.readyReplicas ?? 0)}/{String(row.original.desired ?? 0)} ready</Badge> },
    observed,
  ]
  if (kind === 'disk-pools') return [
    name, health, node,
    { id: 'capacity', header: 'Capacity', accessorFn: (row) => String(row.capacity ?? '—') },
    { id: 'used', header: 'Used', accessorFn: (row) => String(row.used ?? '—') },
    { id: 'available', header: 'Available', accessorFn: (row) => String(row.available ?? '—') },
    { id: 'encrypted', header: 'Encryption', accessorFn: (row) => row.encrypted === true ? 'Enabled' : row.encrypted === false ? 'Disabled' : '—' },
    observed,
  ]
  if (kind.includes('nodes')) return [
    name, health, node,
    { id: 'pool', header: kind.startsWith('lvm') ? 'Volume group' : 'Pool', accessorFn: (row) => String(row.volumeGroup ?? row.pool ?? 'See details') },
    { id: 'capacity', header: 'Capacity', accessorFn: (row) => String(row.capacity ?? '—') },
    { id: 'available', header: 'Available', accessorFn: (row) => String(row.available ?? '—') },
    observed,
  ]
  if (kind.includes('volumes')) return [
    name, health, node,
    { id: 'pool', header: kind === 'lvm-volumes' ? 'Volume group' : kind === 'hostpath-volumes' ? 'StorageClass' : 'Pool', accessorFn: (row) => String(row.volumeGroup ?? row.pool ?? row.storageClass ?? '—') },
    { id: 'capacity', header: 'Capacity', accessorFn: (row) => String(row.capacity ?? '—') },
    { id: 'filesystem', header: 'Filesystem', accessorFn: (row) => String(row.filesystem ?? '—') },
    observed,
  ]
  return [
    name, health, node,
    { id: 'source', header: 'Source', accessorFn: (row) => sourceLabel(row.source) },
    observed,
  ]
}

export function OpenEBSResourcePage() {
  const { providerId = '', kind = '' } = useParams()
  const state = useResourceFilters()
  const query = useProviderResources<OpenEBSResource>(providerId, kind, state.filters)
  const config = resourceConfig[kind] ?? { title: kind.replaceAll('-', ' '), description: 'OpenEBS resources.', guidance: 'Inspect health and freshness before acting.', mode: 'control-plane' as const }
  const rows = query.data?.data ?? []
  const columns = useMemo(() => resourceColumns(kind, providerId), [kind, providerId])
  return <div data-testid="openebs-resource-page">
    <PageHeader title={config.title} description={config.description} actions={<Badge tone="info">read only</Badge>} />
    <div className="mb-4 flex flex-col gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 lg:flex-row lg:items-center lg:justify-between">
      <div className="flex items-start gap-3">
        <div className="rounded-md bg-[var(--color-muted)] p-2 text-[var(--color-primary)]">{config.mode === 'replicated' ? <ShieldCheck size={17} /> : config.mode === 'local' ? <HardDrive size={17} /> : <Activity size={17} />}</div>
        <div><p className="text-sm font-medium">What good looks like</p><p className="mt-0.5 max-w-3xl text-xs text-[var(--color-muted-foreground)]">{config.guidance}</p></div>
      </div>
      <div className="flex items-center gap-2">
        <Input className="w-full lg:w-64" aria-label={`Search ${config.title}`} placeholder={`Search ${config.title.toLowerCase()}`} value={state.filters.search ?? ''} onChange={(event) => state.setSearch(event.target.value)} />
        <Button type="button" variant="outline" size="icon" aria-label="Refresh resources" onClick={() => void query.refetch()}><RefreshCw size={15} /></Button>
      </div>
    </div>
    {config.mode === 'local' ? <Alert tone="warning"><AlertTitle>Node-local storage</AlertTitle><AlertDescription>Availability depends on the owning node. Confirm application-level replication or tested backups before treating this storage as resilient.</AlertDescription></Alert> : null}
    <div className="mt-4">
      <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>
        {rows.length ? <>
          <div className="mb-2 flex justify-between text-xs text-[var(--color-muted-foreground)]"><span>{rows.length} observed resource{rows.length === 1 ? '' : 's'}</span><span>Source-specific, bounded inventory</span></div>
          <DataTable columns={columns} data={rows} tableId={`openebs-${kind}`} getRowId={(row, index) => String(row.id ?? row.name ?? index)} enableExport exportName={`openebs-${kind}`} />
        </> : <Card><CardContent className="py-12 text-center">
          <div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[var(--color-muted)]"><Database size={18} className="text-[var(--color-muted-foreground)]" /></div>
          <p className="text-sm font-medium">No {config.title.toLowerCase()} observed</p>
          <p className="mx-auto mt-1 max-w-lg text-xs text-[var(--color-muted-foreground)]">The engine may not be installed, may not have provisioned resources yet, or its optional CRD may not be served.</p>
        </CardContent></Card>}
      </QueryState>
    </div>
  </div>
}

function detailRows(value: unknown, prefix = '', depth = 0): Array<[string, string]> {
  if (depth > 4 || value === null || value === undefined) return []
  if (Array.isArray(value)) {
    const scalars = value.filter((entry) => ['string', 'number', 'boolean'].includes(typeof entry)).slice(0, 20)
    return scalars.length && prefix ? [[prefix, scalars.join(', ')]] : []
  }
  if (typeof value !== 'object') return prefix ? [[prefix, String(value)]] : []
  const rows: Array<[string, string]> = []
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    if (/secret|password|credential|token|chap|key$/i.test(key)) continue
    const path = prefix ? `${prefix}.${key}` : key
    if (child !== null && typeof child === 'object') rows.push(...detailRows(child, path, depth + 1))
    else rows.push([path, String(child ?? '—')])
    if (rows.length >= 50) break
  }
  return rows.slice(0, 50)
}

function detailLabel(value: string) {
  return value.split('.').pop()?.replaceAll('_', ' ').replace(/([a-z])([A-Z])/g, '$1 $2').replace(/\b\w/g, (character) => character.toUpperCase()) ?? value
}

export function OpenEBSResourceDetailPage() {
  const { providerId = '', kind = '', resourceId = '' } = useParams()
  const id = decodeURIComponent(resourceId)
  const query = useProviderResource<OpenEBSResource>(providerId, kind, id)
  const config = resourceConfig[kind] ?? { title: kind.replaceAll('-', ' '), description: 'OpenEBS resource.', guidance: '', mode: 'control-plane' as const }
  const rows = useMemo(() => detailRows(query.data), [query.data])
  const title = String(query.data?.name ?? id ?? 'OpenEBS resource')
  return <div data-testid="openebs-resource-detail-page">
    <PageHeader title={title} description={`${config.title} detail from ${sourceLabel(query.data?.source)}.`} actions={<Badge tone="info">read only</Badge>} />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>
      {query.data ? <div className="grid gap-4">
        <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="Resource highlights">
          <Highlight label="Health" value={stateValue(query.data)} tone={severityTone(stateValue(query.data).toLowerCase())} />
          <Highlight label="Engine" value={String(query.data.engine ?? 'OpenEBS')} />
          <Highlight label="Node" value={String(query.data.node ?? 'Not reported')} />
          <Highlight label="Observed" value={formatObserved(query.data.observedAt)} />
        </section>
        {config.mode === 'local' ? <Alert tone="warning"><AlertTitle>Local failure boundary</AlertTitle><AlertDescription>This resource depends on its owning node and local storage pool. Review workload replication and restore coverage before node maintenance.</AlertDescription></Alert> : null}
        <Card><CardHeader><CardTitle>Configuration and runtime details</CardTitle></CardHeader><CardContent>
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            {rows.map(([label, value]) => <div key={label} className="min-w-0 border-b border-[var(--color-border)] pb-2"><dt className="text-xs text-[var(--color-muted-foreground)]">{detailLabel(label)}</dt><dd className="mt-1 break-words font-medium">{label.endsWith('observedAt') ? formatObserved(value) : value}</dd></div>)}
          </dl>
        </CardContent></Card>
        <Card><CardHeader><CardTitle>Highland context</CardTitle></CardHeader><CardContent>
          <ResourceContextLink provider={providerId} kind={kind} id={canonicalGraphId(kind, providerId, String(query.data.namespace ?? ''), String(query.data.id ?? id))} />
        </CardContent></Card>
      </div> : null}
    </QueryState>
  </div>
}

function Highlight({ label, value, tone = 'default' }: { label: string; value: string; tone?: 'success' | 'warning' | 'danger' | 'default' }) {
  const color = tone === 'success' ? 'text-[var(--color-success)]' : tone === 'warning' ? 'text-[var(--color-warning)]' : tone === 'danger' ? 'text-[var(--color-destructive)]' : ''
  return <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4"><div className="text-xs text-[var(--color-muted-foreground)]">{label}</div><div className={`mt-2 break-words text-base font-semibold ${color}`}>{value}</div></div>
}
