import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Activity,
  AlertTriangle,
  Check,
  CheckCircle2,
  Copy,
  Database,
  ExternalLink,
  Eye,
  EyeOff,
  Gauge,
  GitBranch,
  HardDrive,
  Layers3,
  Network,
  RefreshCw,
  Server,
  ShieldCheck,
  Workflow,
} from 'lucide-react'
import { useProviderResource, useProviderResources, useProviderSummary, useStorageClaim, useStorageList, useStorageProvider, useStorageProviders, useStorageVolume } from '@/api/storage/hooks'
import type {
  AttachmentSummary,
  CapacitySummary,
  ClaimSummary,
  PersistentVolumeSummary,
  ProviderHealth,
  ProviderDescriptor,
  SnapshotSummary,
  StorageClassSummary,
  StorageEvent,
  StorageFilters,
  StorageCondition,
} from '@/api/storage/types'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { cephDashboardHandoff } from './cephDashboardLinks'
import { storageClient } from '@/api/storage/client'
import { useAuth } from '@/auth/AuthContext'
import { canonicalGraphId } from '@/api/storage/context'
import { ResourceContextLink } from './StorageContextPages'
import { OpenEBSProviderPage } from './OpenEBSStoragePages'
import { LinstorProviderPage } from './LinstorStoragePages'

const LIMIT = 100

function supportTone(level: ProviderDescriptor['supportLevel']) {
  return level === 'managed' ? 'success' : level === 'verified' ? 'info' : 'default'
}

function Health({ provider }: { provider: ProviderDescriptor }) {
  const bad = provider.health.status === 'error' || provider.health.status === 'warning'
  return (
    <span className="inline-flex items-center gap-1.5">
      {bad ? <AlertTriangle size={15} className="text-[var(--color-warning)]" /> : <CheckCircle2 size={15} className="text-[var(--color-success)]" />}
      {provider.health.status}{provider.health.stale ? ' · stale' : ''}
    </span>
  )
}

function PartialConditions({ conditions }: { conditions?: StorageCondition[] }) {
  if (!conditions?.length) return null
  return <div className="mb-4 space-y-2" aria-live="polite">
    {conditions.map((condition) => <Alert key={`${condition.type}-${condition.reason}`} tone={condition.severity === 'error' ? 'danger' : condition.severity === 'warning' ? 'warning' : 'default'}>
      <AlertTitle>{condition.type}: {condition.reason ?? condition.status}</AlertTitle>
      <AlertDescription>{condition.message ?? 'This data source is partially available.'}</AlertDescription>
    </Alert>)}
  </div>
}

export function CephDashboardHandoff({ provider, resourceKind }: { provider: ProviderDescriptor; resourceKind?: string }) {
  const handoff = cephDashboardHandoff(provider, resourceKind)
  const { isAdmin } = useAuth()
  const [credential, setCredential] = useState<{ username: string; password: string }>()
  const [credentialError, setCredentialError] = useState('')
  const [revealing, setRevealing] = useState(false)
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!credential) return
    const timeout = window.setTimeout(() => {
      setCredential(undefined)
      setCopied(false)
    }, 30_000)
    return () => window.clearTimeout(timeout)
  }, [credential])
  if (!handoff) return null
  const availability = provider.metadata?.dashboardAvailability ?? 'unknown'
  async function revealCredential() {
    if (credential) {
      setCredential(undefined)
      setCopied(false)
      return
    }
    setCredentialError('')
    setRevealing(true)
    try {
      setCredential(await storageClient.revealCephDashboardCredential())
    } catch (error) {
      setCredentialError(error instanceof Error ? error.message : 'Credential reveal failed')
    } finally {
      setRevealing(false)
    }
  }
  async function copyCredential() {
    if (!credential) return
    try {
      await navigator.clipboard.writeText(`${credential.username}\n${credential.password}`)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCredentialError('Clipboard access was denied')
    }
  }
  return <Card data-testid="ceph-dashboard-handoff">
    <CardHeader><CardTitle>Native Ceph administration</CardTitle></CardHeader>
    <CardContent className="space-y-2">
      <a
        className="inline-flex items-center gap-2 text-sm font-medium text-[var(--color-primary)] hover:underline"
        href={handoff.href}
        target="_blank"
        rel="noopener noreferrer"
      >
        Open Ceph Dashboard <ExternalLink size={15} />
      </a>
      <p className="text-xs text-[var(--color-muted-foreground)]">
        {handoff.gateway ? 'Opens Ceph through Highland’s authenticated gateway' : 'Opens a separate application'}{handoff.deepLinked ? ' in the relevant Ceph area' : ' at its home page'}. Sign in with your Ceph identity; Highland does not inject its private reader credentials or relay commands.
      </p>
      <p className="text-xs text-[var(--color-muted-foreground)]">
        Private reader status: <span className="font-medium">{availability.replaceAll('-', ' ')}</span>. Highland does not probe the public browser URL.
      </p>
      {isAdmin ? <div className="mt-3 rounded-md border border-[var(--color-border)] p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <p className="text-sm font-medium">Ceph administrator login</p>
            <p className="text-xs text-[var(--color-muted-foreground)]">Admin only · audited · automatically hidden after 30 seconds.</p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={() => void revealCredential()} disabled={revealing}>
            {credential ? <EyeOff size={14} /> : <Eye size={14} />}
            {credential ? 'Hide credential' : revealing ? 'Revealing…' : 'Reveal credential'}
          </Button>
        </div>
        {credential ? <div className="mt-3 flex items-start gap-2">
          <div className="min-w-0 flex-1 rounded-md bg-[var(--color-muted)] p-3 font-mono text-xs">
            <div><span className="text-[var(--color-muted-foreground)]">Username: </span>{credential.username}</div>
            <div className="break-all"><span className="text-[var(--color-muted-foreground)]">Password: </span>{credential.password}</div>
          </div>
          <Button type="button" variant="outline" size="icon" aria-label="Copy Ceph login credential" onClick={() => void copyCredential()}>
            {copied ? <Check size={15} /> : <Copy size={15} />}
          </Button>
        </div> : null}
        {credentialError ? <p role="alert" className="mt-2 text-xs text-[var(--color-destructive)]">{credentialError}</p> : null}
      </div> : null}
      {handoff.insecure ? <Alert tone="warning"><AlertTitle>Disposable-lab HTTP link</AlertTitle><AlertDescription>This dashboard link is not TLS protected. Do not enter production credentials.</AlertDescription></Alert> : null}
    </CardContent>
  </Card>
}

export function StorageProvidersPage() {
  const query = useStorageProviders()
  const providers = query.data?.data ?? []
  return (
    <div data-testid="storage-providers-page">
      <PageHeader title="Storage providers" description="CSI drivers and managed storage backends discovered in this Kubernetes cluster." />
      <PartialConditions conditions={query.data?.meta.conditions} />
      <QueryState
        isLoading={query.isLoading}
        isFetching={query.isFetching && !query.isLoading}
        observedAt={query.data?.meta.observedAt}
        stale={query.data?.meta.stale}
        partial={query.data?.meta.partial}
        error={query.error as Error | null}
        onRetry={() => void query.refetch()}
      >
        {providers.length === 0 ? (
          <Card><CardContent className="py-10 text-center text-sm text-[var(--color-muted-foreground)]">No CSI drivers have been observed yet. Highland will add unknown drivers automatically when a CSIDriver, StorageClass, or CSI-backed PV appears.</CardContent></Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {providers.map((provider) => (
              <Link key={provider.id} to={`/storage/providers/${encodeURIComponent(provider.id)}`} className="rounded-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2">
                <Card className="h-full transition-colors hover:border-[var(--color-primary)]">
                  <CardHeader><CardTitle className="flex items-center justify-between gap-3"><span>{provider.displayName}</span><span className="flex gap-1.5"><Badge tone={supportTone(provider.supportLevel)}>{provider.supportLevel}</Badge>{provider.kind === 'rook-ceph' && !provider.capabilities.some((capability) => capability.startsWith('ceph.') && (capability.endsWith('.create') || capability.endsWith('.delete'))) ? <Badge tone="info">read only</Badge> : null}</span></CardTitle></CardHeader>
                  <CardContent className="space-y-3 text-sm">
                    <Health provider={provider} />
                    <div className="text-[var(--color-muted-foreground)]">{provider.drivers.join(', ') || 'No driver currently observed'}</div>
                    <div className="flex justify-between"><span>Kind</span><span>{provider.kind}</span></div>
                    {provider.version ? <div className="flex justify-between"><span>Version</span><span>{provider.version}</span></div> : null}
                    {provider.namespace ? <div className="flex justify-between"><span>Namespace</span><span>{provider.namespace}</span></div> : null}
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
        )}
      </QueryState>
    </div>
  )
}

export function StorageInventoryPage() {
  const [params] = useSearchParams()
  const provider = params.get('provider') ?? ''
  const query = provider ? `?provider=${encodeURIComponent(provider)}` : ''
  const resources = [
    ['/storage/classes', 'Storage Classes', 'Provisioners, reclaim policy, binding, topology, and expansion.'],
    ['/storage/claims', 'Claims & Workloads', 'PVCs correlated with workloads, PVs, drivers, and attachments.'],
    ['/storage/volumes', 'Persistent Volumes', 'Kubernetes volume truth, reclaim risk, and provider identity.'],
    ['/storage/snapshots', 'Snapshots', 'CSI snapshots, classes, source claims, and readiness.'],
    ['/storage/attachments', 'Attachments', 'Controller attachment state and target nodes.'],
    ['/storage/capacity', 'Capacity', 'Topology-aware CSIStorageCapacity observations.'],
  ] as const
  return <div data-testid="storage-inventory-page">
    <PageHeader title="Storage inventory" description={provider ? `Kubernetes storage resources attributed to ${provider}.` : 'Cross-provider Kubernetes storage resources and their authoritative relationships.'} />
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {resources.map(([path, title, description]) => <Link key={path} to={`${path}${query}`} className="rounded-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2">
        <Card className="h-full transition-colors hover:border-[var(--color-primary)]">
          <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
          <CardContent className="text-sm text-[var(--color-muted-foreground)]">{description}</CardContent>
        </Card>
      </Link>)}
    </div>
  </div>
}

export function StorageProviderPage() {
  const { providerId = '' } = useParams()
  const id = decodeURIComponent(providerId)
  const query = useStorageProvider(id)
  const provider = query.data
  if (provider?.kind === 'rook-ceph') {
    return <RookCephClusterPage provider={provider} isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()} />
  }
  if (provider?.kind === 'openebs') {
    return <OpenEBSProviderPage provider={provider} />
  }
  if (provider?.kind === 'linstor') {
    return <LinstorProviderPage provider={provider} />
  }
  return (
    <div>
      <PageHeader
        title={id === 'longhorn' || provider?.kind === 'longhorn' ? 'Provider details' : 'Dashboard'}
        description={provider
          ? `${provider.displayName} health, capabilities, and authoritative CSI driver ownership.`
          : 'Provider health, capabilities, and authoritative CSI driver ownership.'}
      />
      <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>
        {provider ? <div className="grid gap-4 lg:grid-cols-2">
          <Card><CardHeader><CardTitle>Provider</CardTitle></CardHeader><CardContent className="space-y-2 text-sm">
            <div className="flex justify-between"><span>Support</span><Badge tone={supportTone(provider.supportLevel)}>{provider.supportLevel}</Badge></div>
            <div className="flex justify-between"><span>Health</span><Health provider={provider} /></div>
            <div className="flex justify-between"><span>Drivers</span><span className="text-right">{provider.drivers.join(', ') || '—'}</span></div>
            <div className="flex justify-between"><span>Namespace</span><span>{provider.namespace || 'cluster-wide'}</span></div>
            {provider.version ? <div className="flex justify-between"><span>{provider.kind === 'rook-ceph' ? 'Rook version' : 'Version'}</span><span>{provider.version}</span></div> : null}
            {provider.metadata?.cephVersion ? <div className="flex justify-between"><span>Ceph version</span><span>{provider.metadata.cephVersion}</span></div> : null}
          </CardContent></Card>
          <Card><CardHeader><CardTitle>Capabilities</CardTitle></CardHeader><CardContent className="flex flex-wrap gap-2">
            {provider.capabilities.length ? provider.capabilities.map((cap) => <Badge key={cap} tone="default">{cap}</Badge>) : <span className="text-sm text-[var(--color-muted-foreground)]">No provider-specific capabilities. Common Kubernetes inventory is still available.</span>}
          </CardContent></Card>
          <Card className="lg:col-span-2"><CardHeader><CardTitle>Common Kubernetes inventory</CardTitle></CardHeader><CardContent className="grid gap-2 sm:grid-cols-3"><Link className="rounded-md border border-[var(--color-border)] p-3 text-sm font-medium hover:border-[var(--color-primary)]" to={`/storage/classes?provider=${encodeURIComponent(provider.id)}`}>Storage classes</Link><Link className="rounded-md border border-[var(--color-border)] p-3 text-sm font-medium hover:border-[var(--color-primary)]" to={`/storage/claims?provider=${encodeURIComponent(provider.id)}`}>Claims & workloads</Link><Link className="rounded-md border border-[var(--color-border)] p-3 text-sm font-medium hover:border-[var(--color-primary)]" to={`/storage/volumes?provider=${encodeURIComponent(provider.id)}`}>Persistent volumes</Link></CardContent></Card>
          <Card className="lg:col-span-2"><CardHeader><CardTitle>Highland context layer</CardTitle></CardHeader><CardContent><p className="mb-3 text-sm text-[var(--color-muted-foreground)]">Inspect Kubernetes-to-backend relationships, desired/runtime drift, provider-attributed history, and capacity ownership.</p><Link className="text-sm font-medium text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(provider.id)}/context`}>Open context and insights</Link></CardContent></Card>
          {provider.health.conditions.length ? <Card className="lg:col-span-2"><CardHeader><CardTitle>Conditions</CardTitle></CardHeader><CardContent className="space-y-3">{provider.health.conditions.map((condition) => <div key={`${condition.type}-${condition.reason}`} className="rounded-md border border-[var(--color-border)] p-3"><div className="flex justify-between"><span className="font-medium">{condition.type}</span><Badge tone={condition.severity === 'error' ? 'danger' : condition.severity === 'warning' ? 'warning' : 'default'}>{condition.severity}</Badge></div><p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{condition.message || condition.reason}</p></div>)}</CardContent></Card> : null}
        </div> : null}
      </QueryState>
    </div>
  )
}

type CephSummary = {
  health?: ProviderHealth
  runtimeHealth?: Record<string, unknown>
  runtimeObservedAt?: string
  runtimeStale?: boolean
  metrics?: { values?: Record<string, string>; observedAt?: string; stale?: boolean }
  pools?: Array<Record<string, unknown>>
  filesystems?: Array<Record<string, unknown>>
  osds?: Array<Record<string, unknown>>
  quorum?: Array<Record<string, unknown>>
  conditions?: Array<{ type: string; message?: string; severity: string }>
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function numberValue(value: unknown): number | undefined {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' && value !== '' ? Number(value) : Number.NaN
  return Number.isFinite(parsed) ? parsed : undefined
}

function formatBytes(value: unknown) {
  const bytes = numberValue(value)
  if (bytes === undefined) return 'Unavailable'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const amount = bytes / 1024 ** index
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`
}

function formatRate(value: unknown, suffix = '/s') {
  const rate = numberValue(value)
  return rate === undefined ? 'Unavailable' : `${formatBytes(rate)}${suffix}`
}

function formatObserved(value: unknown) {
  if (typeof value !== 'string' || !value) return 'Not observed'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function percent(used?: number, total?: number) {
  if (used === undefined || total === undefined || total <= 0) return undefined
  return Math.max(0, Math.min(100, (used / total) * 100))
}

function RookCephClusterPage({ provider, isLoading, error, onRetry }: {
  provider: ProviderDescriptor
  isLoading: boolean
  error: Error | null
  onRetry: () => void
}) {
  const query = useProviderSummary<CephSummary>(provider.id)
  const summary = query.data
  const runtimeHealth = summary?.runtimeHealth ?? {}
  const runtimeStatus = String(asRecord(runtimeHealth.health).status ?? runtimeHealth.status ?? 'Unknown')
  const healthy = provider.health.status === 'ok' && runtimeStatus === 'HEALTH_OK'
  const monStatus = asRecord(runtimeHealth.mon_status)
  const quorum = Array.isArray(monStatus.quorum) ? monStatus.quorum.length : undefined
  const mgrMap = asRecord(runtimeHealth.mgr_map)
  const mgrName = typeof mgrMap.active_name === 'string' ? mgrMap.active_name : undefined
  const osdsUp = summary?.osds?.filter((osd) => osd.up === 1 || osd.up === true).length
  const osdsIn = summary?.osds?.filter((osd) => osd.in === 1 || osd.in === true).length
  const osdTotal = summary?.osds?.length
  const dfStats = asRecord(asRecord(runtimeHealth.df).stats)
  const totalBytes = numberValue(dfStats.total_bytes)
  const usedBytes = numberValue(dfStats.total_used_raw_bytes)
  const availableBytes = numberValue(dfStats.total_avail_bytes)
  const usedPercent = percent(usedBytes, totalBytes)
  const pgInfo = asRecord(runtimeHealth.pg_info)
  const pgStatuses = asRecord(pgInfo.statuses)
  const pgSummary = Object.entries(pgStatuses).map(([state, count]) => `${count} ${state}`).join(' · ') || 'Unavailable'
  const objectStats = asRecord(pgInfo.object_stats)
  const objectCount = numberValue(objectStats.num_objects)
  const degraded = numberValue(objectStats.num_objects_degraded)
  const misplaced = numberValue(objectStats.num_objects_misplaced)
  const clientPerf = asRecord(runtimeHealth.client_perf)
  const readRate = numberValue(clientPerf.read_bytes_sec)
  const writeRate = numberValue(clientPerf.write_bytes_sec)
  const issues = provider.health.conditions.filter((condition) => condition.severity === 'error' || condition.severity === 'warning')
  const prometheusCondition = provider.health.conditions.find((condition) => condition.type === 'PrometheusAvailable')
  const dashboardAvailable = provider.metadata?.dashboardAvailability === 'available'
  const dashboardHandoff = cephDashboardHandoff(provider)
  const observedAt = summary?.runtimeObservedAt ?? provider.health.observedAt
  const services = [
    { kind: 'pools', label: 'Block pools', value: `${summary?.pools?.length ?? 0}`, detail: 'Replication, placement groups, and applications', icon: Layers3 },
    { kind: 'osds', label: 'OSDs', value: `${osdsUp ?? 0}/${osdTotal ?? 0} up`, detail: 'Devices that store Ceph data', icon: Server },
    { kind: 'filesystems', label: 'CephFS', value: `${summary?.filesystems?.length ?? 0}`, detail: 'Shared filesystems and metadata servers', icon: Database },
    { kind: 'rbd-images', label: 'RBD images', value: 'View', detail: 'Block images backing Kubernetes volumes', icon: HardDrive },
    { kind: 'quorum', label: 'MON & MGR', value: `${quorum ?? 0} MON · ${mgrName ? `MGR ${mgrName}` : 'no MGR'}`, detail: 'Cluster consensus and management services', icon: ShieldCheck },
    { kind: 'mirroring', label: 'RBD mirroring', value: 'View', detail: 'Cross-cluster image replication status', icon: Network },
  ]

  return <div data-testid="rook-ceph-cluster-page">
    <PageHeader
      title="Dashboard"
      description="Operational health, resilience, capacity, and storage services for this Rook-managed Ceph cluster."
      actions={<Button type="button" variant="outline" size="sm" onClick={() => { void query.refetch(); onRetry() }}><RefreshCw size={14} />Refresh</Button>}
    />
    <QueryState isLoading={isLoading || query.isLoading} error={error ?? query.error as Error | null} onRetry={() => { void query.refetch(); onRetry() }}>
      <section className={`mb-4 overflow-hidden rounded-xl border ${healthy ? 'border-[var(--color-success)]/40' : 'border-[var(--color-warning)]/60'} bg-[var(--color-card)]`}>
        <div className="flex flex-col gap-5 p-5 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-start gap-4">
            <div className={`mt-0.5 rounded-full p-2.5 ${healthy ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]' : 'bg-[var(--color-warning)]/10 text-[var(--color-warning)]'}`}>
              {healthy ? <CheckCircle2 size={24} /> : <AlertTriangle size={24} />}
            </div>
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-xl font-semibold">{healthy ? 'Cluster is healthy' : runtimeStatus.replaceAll('_', ' ')}</h2>
                <Badge tone={healthy ? 'success' : 'warning'}>{runtimeStatus}</Badge>
                {summary?.runtimeStale ? <Badge tone="warning">stale</Badge> : null}
              </div>
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
                {healthy
                  ? `${pgSummary}. ${osdsUp ?? 0} of ${osdTotal ?? 0} OSDs are up and ${osdsIn ?? 0} ${osdsIn === 1 ? 'is' : 'are'} in.`
                  : issues[0]?.message ?? 'One or more Ceph health signals require attention.'}
              </p>
              <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">Runtime observed {formatObserved(observedAt)}</p>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            {dashboardHandoff ? <a href={dashboardHandoff.href} target="_blank" rel="noopener noreferrer" className="inline-flex h-9 items-center gap-2 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-[var(--color-primary-foreground)]">
              Open Ceph Dashboard <ExternalLink size={14} />
            </a> : null}
            <Link to={`/storage/providers/${encodeURIComponent(provider.id)}/context`} className="inline-flex h-9 items-center gap-2 rounded-md border border-[var(--color-border)] px-4 text-sm font-medium hover:bg-[var(--color-accent)]">
              Cluster insights <GitBranch size={14} />
            </Link>
          </div>
        </div>
        {issues.length ? <div className="border-t border-[var(--color-border)] bg-[var(--color-warning)]/5 px-5 py-3">
          <p className="text-sm font-medium">{issues.length} health item{issues.length === 1 ? '' : 's'} need attention</p>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">{issues.map((issue) => issue.message || issue.reason).join(' · ')}</p>
        </div> : null}
      </section>

      <section aria-labelledby="cluster-signals-heading">
        <div className="mb-3 flex items-end justify-between gap-3">
          <div><h2 id="cluster-signals-heading" className="text-base font-semibold">Cluster signals</h2><p className="text-sm text-[var(--color-muted-foreground)]">The six numbers that best describe whether this cluster is safe and usable.</p></div>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <CephSignal icon={Gauge} label="Raw capacity" value={usedPercent === undefined ? 'Unavailable' : `${usedPercent.toFixed(1)}% used`} detail={totalBytes === undefined ? 'Capacity data unavailable' : `${formatBytes(usedBytes)} used · ${formatBytes(availableBytes)} available · ${formatBytes(totalBytes)} total`} progress={usedPercent} warning={usedPercent !== undefined && usedPercent >= 80} />
          <CephSignal icon={Server} label="OSD resilience" value={osdTotal === undefined ? 'Unavailable' : `${osdsUp}/${osdTotal} up · ${osdsIn}/${osdTotal} in`} detail={osdsUp === osdTotal && osdsIn === osdTotal ? 'All storage daemons are serving data' : 'An OSD is down or out of the data set'} warning={osdTotal !== undefined && (osdsUp !== osdTotal || osdsIn !== osdTotal)} />
          <CephSignal icon={ShieldCheck} label="Control plane" value={quorum === undefined ? 'Unavailable' : `${quorum} MON in quorum`} detail={mgrName ? `Manager ${mgrName} is active` : 'No active manager reported'} warning={!mgrName || quorum === 0} />
          <CephSignal icon={Layers3} label="Placement groups" value={pgSummary} detail="Placement groups should remain active+clean" warning={Object.keys(pgStatuses).some((state) => state !== 'active+clean')} />
          <CephSignal icon={Database} label="Objects" value={objectCount === undefined ? 'Unavailable' : objectCount.toLocaleString()} detail={`${(degraded ?? 0).toLocaleString()} degraded · ${(misplaced ?? 0).toLocaleString()} misplaced`} warning={(degraded ?? 0) > 0 || (misplaced ?? 0) > 0} />
          <CephSignal icon={Activity} label="Client I/O" value={`${formatRate(readRate)} read`} detail={`${formatRate(writeRate)} write · ${formatRate(clientPerf.recovering_bytes_per_sec)} recovery`} />
        </div>
      </section>

      <section className="mt-6" aria-labelledby="storage-services-heading">
        <div className="mb-3"><h2 id="storage-services-heading" className="text-base font-semibold">Storage services</h2><p className="text-sm text-[var(--color-muted-foreground)]">Move from cluster health into the Ceph resource responsible for it.</p></div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {services.map((service) => {
            const Icon = service.icon
            return <Link key={service.kind} to={`/storage/providers/${encodeURIComponent(provider.id)}/ceph/${service.kind}`} className="group rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 transition-colors hover:border-[var(--color-primary)]">
              <div className="flex items-start justify-between gap-3">
                <div className="rounded-md bg-[var(--color-muted)] p-2 text-[var(--color-muted-foreground)] group-hover:text-[var(--color-primary)]"><Icon size={18} /></div>
                <span className="text-sm font-semibold">{service.value}</span>
              </div>
              <h3 className="mt-3 text-sm font-semibold">{service.label}</h3>
              <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{service.detail}</p>
            </Link>
          })}
        </div>
      </section>

      <div className="mt-6 grid gap-4 xl:grid-cols-[1.15fr_.85fr]">
        <Card>
          <CardHeader><CardTitle>Cluster configuration</CardTitle></CardHeader>
          <CardContent className="grid gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <ClusterFact label="Rook operator" value={provider.version ?? 'Unknown'} />
            <ClusterFact label="Ceph release" value={provider.metadata?.cephVersion ?? 'Unknown'} />
            <ClusterFact label="Kubernetes namespace" value={provider.namespace ?? 'Unknown'} />
            <ClusterFact label="CSI interfaces" value="RBD block · CephFS file" />
            <ClusterFact label="Dashboard reader" value={dashboardAvailable ? 'Connected' : provider.metadata?.dashboardAvailability?.replaceAll('-', ' ') ?? 'Not configured'} good={dashboardAvailable} />
            <ClusterFact label="Time-series metrics" value={prometheusCondition?.reason === 'NotConfigured' ? 'Not configured' : 'Connected'} good={prometheusCondition?.reason !== 'NotConfigured'} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Highland intelligence</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            <InsightLink to={`/storage/providers/${encodeURIComponent(provider.id)}/context`} icon={GitBranch} title="Context & insights" detail="Trace Kubernetes claims to Ceph resources and inspect drift." />
            <InsightLink to={`/storage/operations?provider=${encodeURIComponent(provider.id)}`} icon={Workflow} title="Operations" detail="Review planned, approved, and completed storage changes." />
            <InsightLink to={`/storage/classes?provider=${encodeURIComponent(provider.id)}`} icon={Layers3} title="Storage classes" detail="Review provisioning policy, reclaim behavior, and expansion." />
            <InsightLink to={`/storage/claims?provider=${encodeURIComponent(provider.id)}`} icon={Database} title="Claims & workloads" detail="See who is consuming Ceph and where workloads run." />
          </CardContent>
        </Card>
      </div>

      <div className="mt-4"><CephDashboardHandoff provider={provider} /></div>
    </QueryState>
  </div>
}

function CephSignal({ icon: Icon, label, value, detail, progress, warning = false }: {
  icon: typeof Gauge
  label: string
  value: string
  detail: string
  progress?: number
  warning?: boolean
}) {
  return <div className={`rounded-lg border bg-[var(--color-card)] p-4 ${warning ? 'border-[var(--color-warning)]/70' : 'border-[var(--color-border)]'}`}>
    <div className="flex items-center justify-between gap-3"><div className="flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)]"><Icon size={15} />{label}</div>{warning ? <AlertTriangle size={15} className="text-[var(--color-warning)]" /> : null}</div>
    <div className="mt-3 text-xl font-semibold tracking-tight">{value}</div>
    {progress !== undefined ? <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-[var(--color-muted)]"><div className={`h-full rounded-full ${warning ? 'bg-[var(--color-warning)]' : 'bg-[var(--color-primary)]'}`} style={{ width: `${progress}%` }} /></div> : null}
    <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">{detail}</p>
  </div>
}

function ClusterFact({ label, value, good }: { label: string; value: string; good?: boolean }) {
  return <div className="flex items-center justify-between gap-4 border-b border-[var(--color-border)] pb-2"><span className="text-[var(--color-muted-foreground)]">{label}</span><span className={`text-right font-medium ${good === false ? 'text-[var(--color-warning)]' : ''}`}>{value}</span></div>
}

function InsightLink({ to, icon: Icon, title, detail }: { to: string; icon: typeof GitBranch; title: string; detail: string }) {
  return <Link to={to} className="flex items-start gap-3 rounded-md border border-transparent p-2 hover:border-[var(--color-border)] hover:bg-[var(--color-accent)]"><Icon size={17} className="mt-0.5 text-[var(--color-primary)]" /><span><span className="block text-sm font-medium">{title}</span><span className="block text-xs text-[var(--color-muted-foreground)]">{detail}</span></span></Link>
}

const cephResourceConfig: Record<string, { title: string; description: string; guidance: string }> = {
  pools: { title: 'Block pools', description: 'Replication, placement groups, application ownership, and Rook reconciliation.', guidance: 'A healthy pool is observed by Ceph, has the intended replication policy, and uses placement-group autoscaling.' },
  osds: { title: 'OSDs', description: 'Storage daemons, host placement, device class, and participation in the data set.', guidance: 'Every expected OSD should be both up (running) and in (receiving data). Investigate either state when it is false.' },
  filesystems: { title: 'CephFS filesystems', description: 'Shared filesystems, metadata-server availability, and pool resilience.', guidance: 'CephFS needs an active metadata server plus healthy metadata and data pools before workloads can mount it safely.' },
  'rbd-images': { title: 'RBD images', description: 'Block images, Kubernetes claim ownership, provisioned size, features, and mirroring.', guidance: 'RBD images back block-mode CSI volumes. Use the PVC identity to connect a Ceph image to its Kubernetes workload.' },
  quorum: { title: 'MON & MGR services', description: 'Consensus, manager availability, placement groups, objects, capacity, and client traffic.', guidance: 'MON quorum protects cluster consensus; the active MGR supplies orchestration, telemetry, and dashboard services.' },
  mirroring: { title: 'RBD mirroring', description: 'Cross-cluster block-image replication configuration and health.', guidance: 'Mirroring is optional. Configure it only when disaster recovery requires images to be replicated to another Ceph cluster.' },
}

function sourceLabel(source: unknown) {
  switch (String(source ?? '')) {
    case 'rook-crd+ceph-dashboard': return 'Rook + Ceph'
    case 'rook-crd': return 'Rook desired state'
    case 'ceph-dashboard': return 'Ceph runtime'
    default: return String(source ?? 'Unknown')
  }
}

function freshnessBadge(row: Record<string, unknown>) {
  const partial = ['unavailable', 'not-observed'].includes(String(row.runtimeState ?? ''))
  return <Badge tone={row.stale || partial ? 'warning' : 'success'}>{row.stale ? 'stale' : partial ? 'partial' : 'current'}</Badge>
}

function stateBadge(value: unknown, healthyValues: string[]) {
  const label = Array.isArray(value) ? value.join(' + ') : String(value ?? 'Unknown')
  const healthy = healthyValues.some((healthyValue) => label.toLowerCase().includes(healthyValue.toLowerCase()))
  return <Badge tone={healthy ? 'success' : 'warning'}>{label}</Badge>
}

function cephResourceColumns(kind: string, providerId: string): ColumnDef<Record<string, unknown>, any>[] {
  const nameColumn: ColumnDef<Record<string, unknown>, any> = {
    id: 'name',
    header: kind === 'rbd-images' ? 'Image' : 'Name',
    cell: ({ row }) => {
      const id = String(row.original.name ?? row.original.id ?? '')
      const label = String(row.original.name ?? row.original.id ?? '—')
      return id ? <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(providerId)}/ceph/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`}>{label}</Link> : label
    },
  }
  const freshness: ColumnDef<Record<string, unknown>, any> = { id: 'freshness', header: 'Freshness', cell: ({ row }) => freshnessBadge(row.original) }
  if (kind === 'pools') return [
    nameColumn,
    { id: 'application', header: 'Application', accessorFn: (row) => {
      const applications = asRecord(row.runtime).application_metadata
      return Array.isArray(applications) ? applications.join(', ') : '—'
    } },
    { id: 'replication', header: 'Replication', accessorFn: (row) => {
      const runtime = asRecord(row.runtime)
      return `${runtime.size ?? asRecord(asRecord(row.spec).replicated).size ?? '—'} replicas · min ${runtime.min_size ?? '—'}`
    } },
    { id: 'pgs', header: 'PGs', accessorFn: (row) => String(asRecord(row.runtime).pg_num ?? '—') },
    { id: 'autoscale', header: 'Autoscale', cell: ({ row }) => stateBadge(asRecord(row.original.runtime).pg_autoscale_mode ?? 'unknown', ['on']) },
    { id: 'source', header: 'Managed by', accessorFn: (row) => sourceLabel(row.source) },
    freshness,
  ]
  if (kind === 'osds') return [
    nameColumn,
    { id: 'health', header: 'Health', cell: ({ row }) => {
      const up = row.original.up === 1 || row.original.up === true
      const inside = row.original.in === 1 || row.original.in === true
      return <Badge tone={up && inside ? 'success' : 'warning'}>{up ? 'up' : 'down'} · {inside ? 'in' : 'out'}</Badge>
    } },
    { id: 'host', header: 'Host', accessorFn: (row) => String(asRecord(row.host).name ?? '—') },
    { id: 'class', header: 'Device class', accessorFn: (row) => String(asRecord(row.tree).device_class ?? row.deviceClass ?? '—') },
    { id: 'weight', header: 'CRUSH weight', accessorFn: (row) => {
      const weight = numberValue(asRecord(row.tree).crush_weight)
      return weight === undefined ? '—' : weight.toFixed(3)
    } },
    freshness,
  ]
  if (kind === 'filesystems') return [
    nameColumn,
    { id: 'health', header: 'Health', cell: ({ row }) => stateBadge(asRecord(row.original.status).phase ?? row.original.state, ['ready']) },
    { id: 'mds', header: 'Active MDS', accessorFn: (row) => String(asRecord(asRecord(row.spec).metadataServer).activeCount ?? '—') },
    { id: 'dataPools', header: 'Data pools', accessorFn: (row) => {
      const pools = asRecord(row.spec).dataPools
      return Array.isArray(pools) ? String(pools.length) : '—'
    } },
    { id: 'dataReplication', header: 'Data replicas', accessorFn: (row) => {
      const pools = asRecord(row.spec).dataPools
      return Array.isArray(pools) && pools.length ? String(asRecord(asRecord(pools[0]).replicated).size ?? '—') : '—'
    } },
    { id: 'metadataReplication', header: 'Metadata replicas', accessorFn: (row) => String(asRecord(asRecord(asRecord(row.spec).metadataPool).replicated).size ?? '—') },
    freshness,
  ]
  if (kind === 'rbd-images') return [
    nameColumn,
    { id: 'pvc', header: 'Kubernetes claim', accessorFn: (row) => {
      const metadata = asRecord(row.metadata)
      const name = metadata['csi.storage.k8s.io/pvc/name']
      const namespace = metadata['csi.storage.k8s.io/pvc/namespace']
      return name ? `${namespace ?? 'default'}/${name}` : 'Unattributed'
    } },
    { id: 'pool', header: 'Pool', accessorFn: (row) => String(row.pool_name ?? '—') },
    { id: 'size', header: 'Provisioned', accessorFn: (row) => formatBytes(row.size) },
    { id: 'objects', header: 'Objects', accessorFn: (row) => numberValue(row.num_objs)?.toLocaleString() ?? '—' },
    { id: 'mirror', header: 'Mirroring', cell: ({ row }) => stateBadge(row.original.mirror_mode ?? 'Disabled', ['enabled', 'pool', 'image']) },
    freshness,
  ]
  return [nameColumn, { id: 'state', header: 'State', cell: ({ row }) => stateBadge(row.original.state ?? asRecord(row.original.status).phase, ['ready', 'health_ok', 'active']) }, { id: 'source', header: 'Source', accessorFn: (row) => sourceLabel(row.source) }, freshness]
}

function CephQuorumDetails({ row }: { row: Record<string, unknown> }) {
  const monStatus = asRecord(row.mon_status)
  const mgrMap = asRecord(row.mgr_map)
  const pgInfo = asRecord(row.pg_info)
  const statuses = asRecord(pgInfo.statuses)
  const objects = asRecord(pgInfo.object_stats)
  const stats = asRecord(asRecord(row.df).stats)
  const perf = asRecord(row.client_perf)
  const total = numberValue(stats.total_bytes)
  const used = numberValue(stats.total_used_raw_bytes)
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    <CephSignal icon={ShieldCheck} label="MON quorum" value={`${Array.isArray(monStatus.quorum) ? monStatus.quorum.length : 0} members`} detail="Consensus members currently participating" warning={!Array.isArray(monStatus.quorum) || monStatus.quorum.length === 0} />
    <CephSignal icon={Server} label="Manager" value={String(mgrMap.active_name ? `MGR ${mgrMap.active_name}` : 'Unavailable')} detail={`${Array.isArray(mgrMap.standbys) ? mgrMap.standbys.length : 0} standby managers`} warning={!mgrMap.active_name} />
    <CephSignal icon={Layers3} label="Placement groups" value={Object.entries(statuses).map(([key, value]) => `${value} ${key}`).join(' · ') || 'Unavailable'} detail="Data placement and recovery state" warning={Object.keys(statuses).some((status) => status !== 'active+clean')} />
    <CephSignal icon={Database} label="Objects" value={numberValue(objects.num_objects)?.toLocaleString() ?? 'Unavailable'} detail={`${numberValue(objects.num_objects_degraded) ?? 0} degraded · ${numberValue(objects.num_objects_unfound) ?? 0} unfound`} warning={(numberValue(objects.num_objects_degraded) ?? 0) > 0 || (numberValue(objects.num_objects_unfound) ?? 0) > 0} />
    <CephSignal icon={Gauge} label="Capacity" value={percent(used, total) === undefined ? 'Unavailable' : `${percent(used, total)?.toFixed(1)}% used`} detail={`${formatBytes(used)} of ${formatBytes(total)}`} progress={percent(used, total)} />
    <CephSignal icon={Activity} label="Client I/O" value={`${formatRate(perf.read_bytes_sec)} read`} detail={`${formatRate(perf.write_bytes_sec)} write · scrub ${String(row.scrub_status ?? 'unknown').toLowerCase()}`} />
  </div>
}

export function CephResourcePage() {
  const { providerId = '', kind = '' } = useParams()
  const state = useFilters()
  const query = useProviderResources<Record<string, unknown>>(providerId, kind, state.filters)
  const rows = query.data?.data ?? []
  const config = cephResourceConfig[kind] ?? { title: kind.replaceAll('-', ' '), description: 'Curated Ceph resource data.', guidance: 'Inspect resource state and freshness before taking action.' }
  const columns = useMemo(() => cephResourceColumns(kind, providerId), [kind, providerId])
  return <div data-testid="ceph-resource-page">
    <PageHeader title={config.title} description={config.description} actions={<Badge tone="info">read only</Badge>} />
    <div className="mb-4 flex flex-col gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 lg:flex-row lg:items-center lg:justify-between">
      <div className="flex items-start gap-3"><div className="rounded-md bg-[var(--color-muted)] p-2 text-[var(--color-primary)]"><ShieldCheck size={17} /></div><div><p className="text-sm font-medium">What good looks like</p><p className="mt-0.5 max-w-3xl text-xs text-[var(--color-muted-foreground)]">{config.guidance}</p></div></div>
      <div className="flex items-center gap-2">
        <Input className="w-full lg:w-64" aria-label={`Search ${config.title}`} placeholder={`Search ${config.title.toLowerCase()}`} value={state.filters.search ?? ''} onChange={(event) => state.set('search', event.target.value)} />
        <Button type="button" variant="outline" size="icon" aria-label="Refresh resources" onClick={() => void query.refetch()}><RefreshCw size={15} /></Button>
      </div>
    </div>
    <QueryState isLoading={query.isLoading} isFetching={query.isFetching && !query.isLoading} observedAt={query.data?.meta?.observedAt} stale={query.data?.meta?.stale} partial={query.data?.meta?.partial} error={query.error as Error | null} onRetry={() => void query.refetch()}>
      {kind === 'quorum' && rows[0] ? <CephQuorumDetails row={rows[0]} /> : rows.length ? <>
        <div className="mb-2 flex items-center justify-between text-xs text-[var(--color-muted-foreground)]"><span>{rows.length} observed resource{rows.length === 1 ? '' : 's'}</span><span>Latest observation {formatObserved(rows[0]?.observedAt)}</span></div>
        <DataTable columns={columns} data={rows} tableId={`ceph-${kind}`} getRowId={(row, index) => String(row.id ?? row.name ?? index)} enableExport exportName={`ceph-${kind}`} />
      </> : <Card><CardContent className="py-12 text-center"><div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[var(--color-muted)]"><Network size={18} className="text-[var(--color-muted-foreground)]" /></div><p className="text-sm font-medium">No {config.title.toLowerCase()} observed</p><p className="mx-auto mt-1 max-w-lg text-xs text-[var(--color-muted-foreground)]">{kind === 'mirroring' ? 'RBD mirroring is not configured for this cluster. That is normal unless your disaster-recovery design includes a second Ceph cluster.' : 'Highland did not receive any resources of this type from Rook or the Ceph Dashboard.'}</p></CardContent></Card>}
    </QueryState>
  </div>
}

const detailKeys = new Set([
  'name', 'id', 'namespace', 'kind', 'apiVersion', 'state', 'phase', 'health',
  'status', 'type', 'host', 'hostname', 'device', 'deviceClass', 'class', 'up',
  'in', 'utilization', 'capacity', 'used', 'available', 'size', 'replicatedSize',
  'failureDomain', 'compressionMode', 'pool', 'poolName', 'filesystem', 'fsName',
  'image', 'imageName', 'mirrorHealth', 'source', 'observedAt', 'stale',
])

function curatedDetails(value: unknown, prefix = '', depth = 0): Array<[string, string]> {
  if (depth > 4 || value === null || value === undefined) return []
  if (Array.isArray(value)) {
    const scalar = value.filter((item) => ['string', 'number', 'boolean'].includes(typeof item)).slice(0, 12)
    return scalar.length ? [[prefix, scalar.join(', ')]] : []
  }
  if (typeof value !== 'object') return prefix ? [[prefix, String(value)]] : []
  const result: Array<[string, string]> = []
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    if (/secret|token|password|credential|key$/i.test(key)) continue
    const path = prefix ? `${prefix}.${key}` : key
    if (child !== null && typeof child === 'object') {
      result.push(...curatedDetails(child, path, depth + 1))
    } else if (detailKeys.has(key) || detailKeys.has(path) || prefix === 'status' || prefix === 'spec') {
      result.push([path, String(child ?? '—')])
    }
    if (result.length >= 40) break
  }
  return result.slice(0, 40)
}

function detailLabel(path: string) {
  const aliases: Record<string, string> = {
    'status.phase': 'Health',
    'spec.replicated.size': 'Replication factor',
    'spec.replicatedSize': 'Replication factor',
    'spec.failureDomain': 'Failure domain',
    'spec.compressionMode': 'Compression',
    'runtime.pg_num': 'Placement groups',
    'runtime.pg_autoscale_mode': 'PG autoscaling',
    'runtime.min_size': 'Minimum replicas',
    pool_name: 'Pool',
    num_objs: 'RADOS objects',
    obj_size: 'Object size',
    mirror_mode: 'Mirroring',
    features_name: 'Features',
    observedAt: 'Observed',
    runtimeState: 'Runtime correlation',
  }
  if (aliases[path]) return aliases[path]
  const leaf = path.split('.').pop() ?? path
  return leaf
    .replaceAll('_', ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (character) => character.toUpperCase())
}

function detailValue(path: string, value: string) {
  if (value === '') return '—'
  if (path === 'source') return sourceLabel(value)
  if (path === 'observedAt') return formatObserved(value)
  if (path === 'stale') return value === 'true' ? 'Stale' : 'Current'
  if (/(^|\.)(size|used|available|capacity|obj_size)$/.test(path)) {
    const parsed = numberValue(value)
    if (parsed !== undefined && parsed >= 1024) return formatBytes(parsed)
  }
  if (value === 'true') return 'Enabled'
  if (value === 'false') return 'Disabled'
  return value
}

function cephDetailHighlights(kind: string, data: Record<string, unknown>): Array<{ label: string; value: string; tone?: 'success' | 'warning' }> {
  const runtime = asRecord(data.runtime)
  const spec = asRecord(data.spec)
  const status = asRecord(data.status)
  if (kind === 'pools') {
    const applications = runtime.application_metadata
    return [
      { label: 'Health', value: String(status.phase ?? (data.runtimeState === 'runtime-only' ? 'Runtime only' : 'Observed')), tone: status.phase === 'Ready' || data.runtimeState === 'observed' ? 'success' : undefined },
      { label: 'Application', value: Array.isArray(applications) ? applications.join(', ') : 'Unassigned' },
      { label: 'Replication', value: `${runtime.size ?? asRecord(spec.replicated).size ?? '—'} replicas · min ${runtime.min_size ?? '—'}` },
      { label: 'Placement groups', value: `${runtime.pg_num ?? '—'} · autoscale ${runtime.pg_autoscale_mode ?? 'unknown'}` },
      { label: 'CRUSH rule', value: String(runtime.crush_rule ?? '—') },
      { label: 'Management source', value: sourceLabel(data.source) },
    ]
  }
  if (kind === 'osds') {
    const up = data.up === 1 || data.up === true
    const inside = data.in === 1 || data.in === true
    return [
      { label: 'Health', value: `${up ? 'Up' : 'Down'} · ${inside ? 'In' : 'Out'}`, tone: up && inside ? 'success' : 'warning' },
      { label: 'Host', value: String(asRecord(data.host).name ?? '—') },
      { label: 'Device class', value: String(asRecord(data.tree).device_class ?? '—') },
      { label: 'CRUSH weight', value: numberValue(asRecord(data.tree).crush_weight)?.toFixed(3) ?? '—' },
      { label: 'UUID', value: String(data.uuid ?? '—') },
      { label: 'Observed', value: formatObserved(data.observedAt) },
    ]
  }
  if (kind === 'filesystems') {
    const dataPools = spec.dataPools
    return [
      { label: 'Health', value: String(status.phase ?? 'Unknown'), tone: status.phase === 'Ready' ? 'success' : 'warning' },
      { label: 'Active MDS', value: String(asRecord(spec.metadataServer).activeCount ?? '—') },
      { label: 'Data pools', value: Array.isArray(dataPools) ? String(dataPools.length) : '—' },
      { label: 'Data replication', value: Array.isArray(dataPools) && dataPools.length ? String(asRecord(asRecord(dataPools[0]).replicated).size ?? '—') : '—' },
      { label: 'Metadata replication', value: String(asRecord(asRecord(spec.metadataPool).replicated).size ?? '—') },
      { label: 'Namespace', value: String(data.namespace ?? '—') },
    ]
  }
  if (kind === 'rbd-images') {
    const metadata = asRecord(data.metadata)
    return [
      { label: 'Kubernetes claim', value: metadata['csi.storage.k8s.io/pvc/name'] ? `${metadata['csi.storage.k8s.io/pvc/namespace'] ?? 'default'}/${metadata['csi.storage.k8s.io/pvc/name']}` : 'Unattributed' },
      { label: 'Pool', value: String(data.pool_name ?? '—') },
      { label: 'Provisioned size', value: formatBytes(data.size) },
      { label: 'RADOS objects', value: numberValue(data.num_objs)?.toLocaleString() ?? '—' },
      { label: 'Features', value: Array.isArray(data.features_name) ? data.features_name.join(', ') : '—' },
      { label: 'Mirroring', value: String(data.mirror_mode ?? 'Disabled') },
    ]
  }
  return [
    { label: 'State', value: String(data.state ?? status.phase ?? 'Unknown') },
    { label: 'Source', value: sourceLabel(data.source) },
    { label: 'Observed', value: formatObserved(data.observedAt) },
  ]
}

export function CephResourceDetailPage() {
  const { providerId = '', kind = '', resourceId = '' } = useParams()
  const id = decodeURIComponent(resourceId)
  const query = useProviderResource<Record<string, unknown>>(providerId, kind, id)
  const providerQuery = useStorageProvider(providerId)
  const details = useMemo(() => curatedDetails(query.data), [query.data])
  const highlights = useMemo(() => query.data ? cephDetailHighlights(kind, query.data) : [], [kind, query.data])
  const title = String(query.data?.name ?? query.data?.id ?? (id || 'Ceph resource'))
  const config = cephResourceConfig[kind] ?? { title: kind.replaceAll('-', ' '), description: 'Curated Ceph resource data.', guidance: '' }
  return <div data-testid="ceph-resource-detail-page">
    <PageHeader title={title} description={`${config.title} detail from Rook desired state and the Ceph runtime.`} actions={<Badge tone="info">read only</Badge>} />
    <QueryState
      isLoading={query.isLoading}
      isFetching={query.isFetching && !query.isLoading}
      error={query.error as Error | null}
      onRetry={() => void query.refetch()}
    >
      <div className="grid gap-4">
      {query.data ? <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" aria-label="Resource highlights">
        {highlights.map((highlight) => <div key={highlight.label} className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <div className="text-xs font-medium text-[var(--color-muted-foreground)]">{highlight.label}</div>
          <div className={`mt-2 break-words text-base font-semibold ${highlight.tone === 'success' ? 'text-[var(--color-success)]' : highlight.tone === 'warning' ? 'text-[var(--color-warning)]' : ''}`}>{highlight.value}</div>
        </div>)}
      </section> : null}
      <Card>
        <CardHeader><CardTitle>Configuration & runtime details</CardTitle></CardHeader>
        <CardContent>
          {details.length ? <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            {details.map(([label, value]) => <div key={label} className="min-w-0 border-b border-[var(--color-border)] pb-2"><dt className="text-xs text-[var(--color-muted-foreground)]">{detailLabel(label)}</dt><dd className="mt-1 break-words text-sm font-medium">{detailValue(label, value)}</dd></div>)}
          </dl> : <p className="text-sm text-[var(--color-muted-foreground)]">No supported detail fields were returned for this resource.</p>}
        </CardContent>
      </Card>
      {query.data ? <Card><CardHeader><CardTitle>Highland context</CardTitle></CardHeader><CardContent><ResourceContextLink
        provider={providerId}
        kind={cephGraphKind(kind)}
        id={canonicalGraphId(cephGraphKind(kind), providerId, String(query.data.namespace ?? ''), String(query.data.id ?? id))}
      /></CardContent></Card> : null}
      {providerQuery.data?.kind === 'rook-ceph' ? <CephDashboardHandoff provider={providerQuery.data} resourceKind={kind} /> : null}
      </div>
    </QueryState>
  </div>
}

function cephGraphKind(kind: string) {
  const kinds: Record<string, string> = {
    clusters: 'ceph-cluster',
    pools: 'ceph-block-pool',
    filesystems: 'ceph-filesystem',
    mirroring: 'ceph-rbd-mirror',
    osds: 'osd',
    'rbd-images': 'rbd-image',
  }
  return kinds[kind] ?? kind
}

function useFilters() {
  const [params, setParams] = useSearchParams()
  const filters: StorageFilters = {
    provider: params.get('provider') || undefined,
    driver: params.get('driver') || undefined,
    namespace: params.get('namespace') || undefined,
    status: params.get('status') || undefined,
    search: params.get('search') || undefined,
    continue: params.get('continue') || undefined,
    limit: LIMIT,
  }
  const set = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value); else next.delete(key)
    if (key !== 'continue') next.delete('continue')
    setParams(next)
  }
  return { filters, params, setParams, set }
}

function FilterBar({ filters, set }: ReturnType<typeof useFilters>) {
  return <div className="mb-4 grid gap-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-3 sm:grid-cols-2 lg:grid-cols-5" aria-label="Storage inventory filters">
    <Input aria-label="Search" placeholder="Search" value={filters.search ?? ''} onChange={(e) => set('search', e.target.value)} />
    <Input aria-label="Provider" placeholder="Provider" value={filters.provider ?? ''} onChange={(e) => set('provider', e.target.value)} />
    <Input aria-label="Driver" placeholder="CSI driver" value={filters.driver ?? ''} onChange={(e) => set('driver', e.target.value)} />
    <Input aria-label="Namespace" placeholder="Namespace" value={filters.namespace ?? ''} onChange={(e) => set('namespace', e.target.value)} />
    <Input aria-label="Status" placeholder="Status" value={filters.status ?? ''} onChange={(e) => set('status', e.target.value)} />
  </div>
}

function InventoryPage<T>({ kind, title, description, columns, rowId, capabilityEmpty }: {
  kind: 'classes' | 'claims' | 'volumes' | 'snapshots' | 'attachments' | 'capacity' | 'events'
  title: string
  description: string
  columns: ColumnDef<T, any>[]
  rowId: (row: T) => string
  capabilityEmpty?: ReactNode
}) {
  const state = useFilters()
  const query = useStorageList<T>(kind, state.filters)
  const page = query.data
  const data = page?.data ?? []
  const advance = (token?: string) => {
    const next = new URLSearchParams(state.params)
    if (token) next.set('continue', token); else next.delete('continue')
    state.setParams(next)
  }
  return <div data-testid={`storage-${kind}-page`}>
    <PageHeader title={title} description={description} />
    <FilterBar {...state} />
    <PartialConditions conditions={page?.conditions} />
    <QueryState
      isLoading={query.isLoading}
      isFetching={query.isFetching && !query.isLoading}
      observedAt={page?.meta?.observedAt}
      stale={page?.meta?.stale}
      partial={page?.meta?.partial}
      error={query.error as Error | null}
      onRetry={() => void query.refetch()}
    >
      {data.length === 0 && capabilityEmpty ? capabilityEmpty : <DataTable columns={columns} data={data} tableId={`storage-${kind}`} getRowId={rowId} enableExport exportName={`highland-${kind}`} />}
      <div className="mt-3 flex items-center justify-between text-sm text-[var(--color-muted-foreground)]">
        <span>{page?.page.total ?? 0} matching objects in the current cache</span>
        <div className="flex gap-2"><Button variant="outline" size="sm" disabled={!state.filters.continue} onClick={() => advance()}>First page</Button><Button variant="outline" size="sm" disabled={!page?.page.continue} onClick={() => advance(page?.page.continue)}>Next page</Button></div>
      </div>
    </QueryState>
  </div>
}

const conditionCell = (conditions?: { severity: string; message?: string }[]) => conditions?.some((c) => c.severity === 'error' || c.severity === 'warning') ? <Badge tone="warning">Needs attention</Badge> : <Badge tone="success">OK</Badge>

export function StorageClassesPage() {
  const columns = useMemo<ColumnDef<StorageClassSummary, any>[]>(() => [
    { id: 'name', header: 'Name', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/relationships/storageclass/${encodeURIComponent(canonicalGraphId('storageclass', row.original.providerId, '', row.original.name))}?provider=${encodeURIComponent(row.original.providerId)}`}>{row.original.name}</Link> }, { accessorKey: 'provisioner', header: 'Provisioner' }, { accessorKey: 'providerId', header: 'Provider' },
    { accessorKey: 'reclaimPolicy', header: 'Reclaim' }, { accessorKey: 'volumeBindingMode', header: 'Binding' },
    { id: 'usage', header: 'Claims / PVs', cell: ({ row }) => `${row.original.claimCount} / ${row.original.volumeCount}` },
    { id: 'expansion', header: 'Expansion', cell: ({ row }) => row.original.allowVolumeExpansion ? 'Allowed' : 'Disabled' },
  ], [])
  return <InventoryPage kind="classes" title="Storage classes" description="Provisioning profiles grouped by authoritative CSI provisioner." columns={columns} rowId={(row) => row.name} />
}

export function StorageClaimsPage() {
  const [params] = useSearchParams()
  const providerQuery = params.get('provider') ? `?provider=${encodeURIComponent(params.get('provider') ?? '')}` : ''
  const columns = useMemo<ColumnDef<ClaimSummary, any>[]>(() => [
    { id: 'claim', header: 'Claim', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/claims/${encodeURIComponent(row.original.namespace)}/${encodeURIComponent(row.original.name)}${providerQuery}`}>{row.original.namespace}/{row.original.name}</Link> }, { accessorKey: 'phase', header: 'Phase' },
    { accessorKey: 'storageClass', header: 'Storage class' }, { accessorKey: 'requestedCapacity', header: 'Requested' }, { accessorKey: 'provisionedCapacity', header: 'Provisioned' },
    { accessorKey: 'driver', header: 'Driver' }, { id: 'workloads', header: 'Workloads', cell: ({ row }) => row.original.workloads?.length ?? 0 },
    { id: 'health', header: 'Conditions', cell: ({ row }) => conditionCell(row.original.conditions) },
  ], [providerQuery])
  return <InventoryPage kind="claims" title="Claims & workloads" description="PVCs correlated to PVs, CSI identities, pods, controllers, and attachments." columns={columns} rowId={(row) => row.id} />
}

export function StorageVolumesPage() {
  const [params] = useSearchParams()
  const providerQuery = params.get('provider') ? `?provider=${encodeURIComponent(params.get('provider') ?? '')}` : ''
  const columns = useMemo<ColumnDef<PersistentVolumeSummary, any>[]>(() => [
    { id: 'name', header: 'PV', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/volumes/${encodeURIComponent(row.original.name)}${providerQuery}`}>{row.original.name}</Link> }, { accessorKey: 'phase', header: 'Phase' }, { accessorKey: 'capacity', header: 'Provisioned' },
    { accessorKey: 'storageClass', header: 'Storage class' }, { accessorKey: 'driver', header: 'Driver' }, { accessorKey: 'reclaimPolicy', header: 'Reclaim' },
    { id: 'claim', header: 'Claim', accessorFn: (r) => r.claimName ? `${r.claimNamespace}/${r.claimName}` : '—' },
    { id: 'health', header: 'Conditions', cell: ({ row }) => conditionCell(row.original.conditions) },
  ], [providerQuery])
  return <InventoryPage kind="volumes" title="Persistent volumes" description="Kubernetes PV truth with provider correlation and explicit orphan risk." columns={columns} rowId={(row) => row.name} />
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return <div className="min-w-0"><dt className="text-xs text-[var(--color-muted-foreground)]">{label}</dt><dd className="mt-1 break-words text-sm font-medium">{children || '—'}</dd></div>
}

function ProviderReferenceLink({ providerId, reference }: { providerId: string; reference?: { kind: string; id: string } }) {
  if (!reference) return <span className="text-[var(--color-muted-foreground)]">Backend mapping unavailable</span>
  if (reference.kind === 'longhorn-volume') return <Link className="text-[var(--color-primary)] hover:underline" to={`/volumes/${encodeURIComponent(reference.id)}`}>{reference.kind}/{reference.id}</Link>
  if (reference.kind === 'ceph-rbd-image') return <Link className="text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(providerId)}/ceph/rbd-images/${encodeURIComponent(reference.id)}`}>{reference.kind}/{reference.id}</Link>
  if (reference.kind === 'linstor-resource') return <Link className="text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(providerId)}/linstor/resource-definitions/${encodeURIComponent(reference.id)}`}>{reference.kind}/{reference.id}</Link>
  const openEBSKinds: Record<string, string> = {
    'openebs-hostpath-volume': 'hostpath-volumes',
    'openebs-lvm-volume': 'lvm-volumes',
    'openebs-zfs-volume': 'zfs-volumes',
  }
  if (openEBSKinds[reference.kind]) return <Link className="text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(providerId)}/openebs/${openEBSKinds[reference.kind]}/${encodeURIComponent(reference.id)}`}>{reference.kind}/{reference.id}</Link>
  return <span>{reference.kind}/{reference.id}</span>
}

function DetailConditions({ conditions }: { conditions?: StorageCondition[] }) {
  if (!conditions?.length) return <p className="text-sm text-[var(--color-muted-foreground)]">No active storage conditions.</p>
  return <div className="space-y-2">{conditions.map((condition) => <Alert key={`${condition.type}-${condition.reason}`} tone={condition.severity === 'error' ? 'danger' : condition.severity === 'warning' ? 'warning' : 'default'}><AlertTitle>{condition.type}: {condition.reason ?? condition.status}</AlertTitle><AlertDescription>{condition.message ?? condition.status}</AlertDescription></Alert>)}</div>
}

export function StorageClaimDetailPage() {
  const { namespace = '', name = '' } = useParams()
  const decodedNamespace = decodeURIComponent(namespace)
  const decodedName = decodeURIComponent(name)
  const query = useStorageClaim(decodedNamespace, decodedName)
  const claim = query.data
  return <div data-testid="storage-claim-detail-page">
    <PageHeader title={claim ? `${claim.namespace}/${claim.name}` : 'Persistent volume claim'} description="Kubernetes claim identity correlated with its PV, workloads, attachments, and authoritative provider reference." />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>{claim ? <div className="grid gap-4 lg:grid-cols-2">
      <Card><CardHeader><CardTitle>Claim</CardTitle></CardHeader><CardContent><dl className="grid gap-4 sm:grid-cols-2"><Fact label="Phase"><Badge tone={claim.phase === 'Bound' ? 'success' : 'default'}>{claim.phase}</Badge></Fact><Fact label="StorageClass">{claim.storageClass}</Fact><Fact label="Requested">{claim.requestedCapacity}</Fact><Fact label="Provisioned">{claim.provisionedCapacity}</Fact><Fact label="Access modes">{claim.accessModes?.join(', ')}</Fact><Fact label="Volume mode">{claim.volumeMode}</Fact><Fact label="CSI driver">{claim.driver}</Fact><Fact label="Provider">{claim.providerId}</Fact></dl></CardContent></Card>
      <Card><CardHeader><CardTitle>Volume and backend</CardTitle></CardHeader><CardContent><dl className="grid gap-4 sm:grid-cols-2"><Fact label="PersistentVolume">{claim.pvName ? <Link className="text-[var(--color-primary)] hover:underline" to={`/storage/volumes/${encodeURIComponent(claim.pvName)}?provider=${encodeURIComponent(claim.providerId)}`}>{claim.pvName}</Link> : 'Not bound'}</Fact><Fact label="Reclaim policy">{claim.reclaimPolicy}</Fact><Fact label="Volume handle">{claim.volumeHandle}</Fact><Fact label="Provider reference"><ProviderReferenceLink providerId={claim.providerId} reference={claim.providerRef} /></Fact></dl></CardContent></Card>
      <Card><CardHeader><CardTitle>Workloads</CardTitle></CardHeader><CardContent>{claim.workloads?.length ? <ul className="space-y-3">{claim.workloads.map((workload) => <li key={`${workload.namespace}/${workload.podName}/${workload.name}`} className="rounded-md border border-[var(--color-border)] p-3 text-sm"><div className="font-medium">{workload.kind} {workload.namespace}/{workload.name}</div><div className="mt-1 text-[var(--color-muted-foreground)]">Pod {workload.podName} · {workload.podPhase}{workload.nodeName ? ` · node ${workload.nodeName}` : ''}</div></li>)}</ul> : <p className="text-sm text-[var(--color-muted-foreground)]">No observed pod currently references this claim.</p>}</CardContent></Card>
      <Card><CardHeader><CardTitle>Attachments</CardTitle></CardHeader><CardContent>{claim.attachmentIds?.length ? <ul className="space-y-2">{claim.attachmentIds.map((id) => <li key={id}><Link className="text-sm text-[var(--color-primary)] hover:underline" to={`/storage/attachments?provider=${encodeURIComponent(claim.providerId)}&search=${encodeURIComponent(id)}`}>{id}</Link></li>)}</ul> : <p className="text-sm text-[var(--color-muted-foreground)]">No VolumeAttachment currently references the bound PV.</p>}</CardContent></Card>
      <Card className="lg:col-span-2"><CardHeader><CardTitle>Context and impact</CardTitle></CardHeader><CardContent><ResourceContextLink provider={claim.providerId} kind="pvc" id={canonicalGraphId('pvc', claim.providerId, claim.namespace, claim.name)} /></CardContent></Card>
      <Card className="lg:col-span-2"><CardHeader><CardTitle>Conditions</CardTitle></CardHeader><CardContent><DetailConditions conditions={claim.conditions} /></CardContent></Card>
    </div> : null}</QueryState>
  </div>
}

export function StorageVolumeDetailPage() {
  const { name = '' } = useParams()
  const decodedName = decodeURIComponent(name)
  const query = useStorageVolume(decodedName)
  const volume = query.data
  return <div data-testid="storage-volume-detail-page">
    <PageHeader title={volume?.name ?? 'Persistent volume'} description="Kubernetes PV truth, claim linkage, CSI identity, attachments, and provider correlation." />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>{volume ? <div className="grid gap-4 lg:grid-cols-2">
      <Card><CardHeader><CardTitle>PersistentVolume</CardTitle></CardHeader><CardContent><dl className="grid gap-4 sm:grid-cols-2"><Fact label="Phase"><Badge tone={volume.phase === 'Bound' ? 'success' : 'default'}>{volume.phase}</Badge></Fact><Fact label="Capacity">{volume.capacity}</Fact><Fact label="StorageClass">{volume.storageClass}</Fact><Fact label="Reclaim policy">{volume.reclaimPolicy}</Fact><Fact label="CSI driver">{volume.driver}</Fact><Fact label="Provider">{volume.providerId}</Fact></dl></CardContent></Card>
      <Card><CardHeader><CardTitle>Relationships</CardTitle></CardHeader><CardContent><dl className="grid gap-4 sm:grid-cols-2"><Fact label="Claim">{volume.claimName && volume.claimNamespace ? <Link className="text-[var(--color-primary)] hover:underline" to={`/storage/claims/${encodeURIComponent(volume.claimNamespace)}/${encodeURIComponent(volume.claimName)}?provider=${encodeURIComponent(volume.providerId)}`}>{volume.claimNamespace}/{volume.claimName}</Link> : 'Unclaimed'}</Fact><Fact label="Volume handle">{volume.volumeHandle}</Fact><Fact label="Provider reference"><ProviderReferenceLink providerId={volume.providerId} reference={volume.providerRef} /></Fact><Fact label="Backend allocated">{volume.backendAllocatedCapacity}</Fact></dl></CardContent></Card>
      <Card><CardHeader><CardTitle>Attachments</CardTitle></CardHeader><CardContent>{volume.attachmentIds?.length ? <ul className="space-y-2">{volume.attachmentIds.map((id) => <li key={id}><Link className="text-sm text-[var(--color-primary)] hover:underline" to={`/storage/attachments?provider=${encodeURIComponent(volume.providerId)}&search=${encodeURIComponent(id)}`}>{id}</Link></li>)}</ul> : <p className="text-sm text-[var(--color-muted-foreground)]">No VolumeAttachment currently references this PV.</p>}</CardContent></Card>
      <Card><CardHeader><CardTitle>Conditions</CardTitle></CardHeader><CardContent><DetailConditions conditions={volume.conditions} /></CardContent></Card>
      <Card className="lg:col-span-2"><CardHeader><CardTitle>Context and impact</CardTitle></CardHeader><CardContent><ResourceContextLink provider={volume.providerId} kind="pv" id={canonicalGraphId('pv', volume.providerId, '', volume.name)} /></CardContent></Card>
    </div> : null}</QueryState>
  </div>
}

export function StorageSnapshotsPage() {
  const columns = useMemo<ColumnDef<SnapshotSummary, any>[]>(() => [
    { id: 'snapshot', header: 'Snapshot', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/relationships/volumesnapshot/${encodeURIComponent(canonicalGraphId('volumesnapshot', row.original.providerId, row.original.namespace, row.original.name))}?provider=${encodeURIComponent(row.original.providerId)}`}>{row.original.namespace}/{row.original.name}</Link> }, { accessorKey: 'snapshotClass', header: 'Class' },
    { accessorKey: 'sourcePvc', header: 'Source claim' }, { accessorKey: 'driver', header: 'Driver' }, { accessorKey: 'restoreSize', header: 'Restore size' },
    { id: 'ready', header: 'Ready', cell: ({ row }) => row.original.readyToUse === undefined ? 'Unknown' : row.original.readyToUse ? 'Yes' : 'No' },
  ], [])
  return <InventoryPage kind="snapshots" title="Volume snapshots" description="CSI snapshots and their Kubernetes source claims and classes." columns={columns} rowId={(row) => row.id} capabilityEmpty={<Card><CardContent className="py-10 text-center"><Database className="mx-auto mb-3 text-[var(--color-muted-foreground)]" /><p className="font-medium">No snapshots are available</p><p className="mt-1 text-sm text-[var(--color-muted-foreground)]">The snapshot.storage.k8s.io/v1 API may be absent, permissions may be partial, or no snapshots exist.</p></CardContent></Card>} />
}

export function StorageAttachmentsPage() {
  const columns = useMemo<ColumnDef<AttachmentSummary, any>[]>(() => [
    { id: 'name', header: 'Attachment', cell: ({ row }) => <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/relationships/volumeattachment/${encodeURIComponent(canonicalGraphId('volumeattachment', row.original.providerId, '', row.original.name))}?provider=${encodeURIComponent(row.original.providerId)}`}>{row.original.name}</Link> }, { accessorKey: 'pvName', header: 'PV' }, { accessorKey: 'nodeName', header: 'Node' },
    { accessorKey: 'driver', header: 'Driver' }, { accessorKey: 'providerId', header: 'Provider' }, { id: 'state', header: 'State', cell: ({ row }) => <Badge tone={row.original.attached ? 'success' : 'default'}>{row.original.attached ? 'Attached' : 'Detached'}</Badge> },
  ], [])
  return <InventoryPage kind="attachments" title="Volume attachments" description="CSI controller attachment state, kept distinct from backend replica placement." columns={columns} rowId={(row) => row.name} />
}

export function StorageCapacityPage() {
  const columns = useMemo<ColumnDef<CapacitySummary, any>[]>(() => [
    { accessorKey: 'storageClass', header: 'Storage class' }, { accessorKey: 'capacity', header: 'Available' }, { accessorKey: 'maximumVolumeSize', header: 'Maximum volume' },
    { accessorKey: 'driver', header: 'Driver' }, { accessorKey: 'providerId', header: 'Provider' }, { accessorKey: 'observedAt', header: 'Observed' },
  ], [])
  return <InventoryPage kind="capacity" title="CSI capacity" description="Topology-aware CSIStorageCapacity objects; values remain exact Kubernetes quantity strings." columns={columns} rowId={(row) => `${row.providerId}/${row.driver}/${row.storageClass}/${row.observedAt}`} />
}

export function StorageEventsPage() {
  const columns = useMemo<ColumnDef<StorageEvent, any>[]>(() => [
    { accessorKey: 'type', header: 'Type' }, { accessorKey: 'reason', header: 'Reason' }, { accessorKey: 'message', header: 'Message' },
    { id: 'regarding', header: 'Regarding', accessorFn: (r) => `${r.regardingKind ?? ''}/${r.regardingName ?? ''}` }, { accessorKey: 'namespace', header: 'Namespace' }, { accessorKey: 'lastObservedAt', header: 'Last observed' },
  ], [])
  return <InventoryPage kind="events" title="Storage events" description="Kubernetes events involving storage resources and CSI reconciliation." columns={columns} rowId={(row) => `${row.namespace}/${row.name}`} />
}
