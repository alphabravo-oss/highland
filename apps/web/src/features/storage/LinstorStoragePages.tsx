import { useMemo } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import { Activity, AlertTriangle, Boxes, CheckCircle2, Database, GitBranch, HardDrive, Network, RefreshCw, Server } from 'lucide-react'
import { useProviderResource, useProviderResources, useProviderSummary } from '@/api/storage/hooks'
import type { ProviderDescriptor, StorageCondition, StorageFilters } from '@/api/storage/types'
import { DataTable } from '@/components/data/DataTable'
import { Donut, LegendRow, UsageBar } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { ProviderWorkloadFootprint } from './ProviderWorkloadFootprint'

type Component = { id: string; name: string; kind: string; desired: number; readyReplicas: number; ready: boolean }
type Summary = { namespace: string; components: Component[]; resourceCounts: Record<string, number>; conditions?: StorageCondition[]; managementMode: string }
type Resource = Record<string, unknown> & { id?: string; name?: string; source?: string; node_name?: string; node?: string; state?: unknown; status?: unknown; health?: string; ready?: boolean; provider_kind?: string; free_capacity?: number; total_capacity?: number; observedAt?: string; volumes?: Array<{ state?: { disk_state?: string } }> }

const config: Record<string, { title: string; description: string; guidance: string }> = {
  components: { title: 'Control-plane components', description: 'Piraeus Operator, LINSTOR controller, satellite, CSI, and DRBD workload readiness.', guidance: 'Every expected workload should have all desired replicas ready before maintenance or provisioning.' },
  clusters: { title: 'Piraeus clusters', description: 'Declarative LinstorCluster resources observed from Kubernetes.', guidance: 'The cluster Available condition should be true and all configured controller and satellite replicas should converge.' },
  satellites: { title: 'Piraeus satellites', description: 'Per-node satellite status reported by the Piraeus Operator.', guidance: 'Every storage node should have an available satellite and current configuration.' },
  'satellite-configurations': { title: 'Satellite configurations', description: 'Host matching, storage-pool, kernel-module, and satellite configuration policy.', guidance: 'Confirm selectors apply to the intended nodes and status reports no rollout errors.' },
  'node-connections': { title: 'Node connections', description: 'Declarative Piraeus node-to-node connection policy.', guidance: 'Review explicit paths and networking overrides whenever replication links are degraded.' },
  nodes: { title: 'LINSTOR nodes', description: 'Controller-authoritative satellite connectivity and node state.', guidance: 'Production nodes should be Online; investigate Offline, Unknown, or Evicted nodes before provisioning.' },
  'storage-pools': { title: 'Storage pools', description: 'Controller-authoritative allocatable capacity by node and provider.', guidance: 'Pools should be healthy and retain enough free capacity for replica placement and recovery headroom.' },
  'resource-groups': { title: 'Resource groups', description: 'Placement, replica-count, storage-pool, and failure-domain policy.', guidance: 'Treat resource groups as policy templates; verify replica and placement constraints match workload durability.' },
  'resource-definitions': { title: 'Resource definitions', description: 'Logical LINSTOR resources and volume definitions, including CSI correlation metadata.', guidance: 'Each Kubernetes LINSTOR PV should correlate to exactly one resource definition and volume number.' },
  resources: { title: 'Resource replicas', description: 'Runtime resource placement and disk state across satellites.', guidance: 'Expected replicas should be diskful, UpToDate, and distributed across the intended failure domains.' },
  snapshots: { title: 'LINSTOR snapshots', description: 'Backend snapshot state and placement.', guidance: 'A useful snapshot must be complete, restorable, and covered by an independently tested retention policy.' },
  remotes: { title: 'Backup remotes', description: 'Configured LINSTOR, S3, and cloud backup targets.', guidance: 'Verify endpoint trust, credential ownership, retention, and a tested restore path outside Highland.' },
  schedules: { title: 'Backup schedules', description: 'Controller backup schedule definitions.', guidance: 'Confirm schedules are attached to the intended resource groups and that recent executions succeed.' },
  'error-reports': { title: 'Error reports', description: 'Bounded LINSTOR controller and satellite diagnostic reports.', guidance: 'Use report IDs and timestamps to correlate faults; collect full diagnostics directly from LINSTOR when escalating.' },
}

function tone(status: string): 'success' | 'warning' | 'danger' | 'default' {
  const value = status.toLowerCase()
  if (/ok|online|ready|available|uptodate|true/.test(value)) return 'success'
  if (/error|offline|failed|fault|false/.test(value)) return 'danger'
  if (/warn|degrad|unknown|pending/.test(value)) return 'warning'
  return 'default'
}
function state(resource: Resource) {
  if (typeof resource.health === 'string') return resource.health
  if (typeof resource.state === 'string') return resource.state
  if (typeof resource.status === 'string') return resource.status
  if (resource.ready !== undefined) return resource.ready ? 'Ready' : 'Not ready'
  const nested = resource.volumes?.find((volume) => volume.state?.disk_state)?.state?.disk_state
  return nested ?? String(resource.connection_status ?? resource.disk_state ?? 'Observed')
}
function formatCapacity(value: unknown, diskless = false) { const n = Number(value); if (diskless || !Number.isFinite(n) || n <= 0 || n > Number.MAX_SAFE_INTEGER) return '—'; return `${(n / 1024 / 1024).toFixed(1)} GiB` }

export function LinstorProviderPage({ provider }: { provider: ProviderDescriptor }) {
  const query = useProviderSummary<Summary>(provider.id)
  const poolsQuery = useProviderResources<Resource>(provider.id, 'storage-pools', { limit: 100 })
  const resourcesQuery = useProviderResources<Resource>(provider.id, 'resources', { limit: 100 })
  const data = query.data
  const ready = data?.components.filter((item) => item.ready).length ?? 0
  const count = (kind: string) => data?.resourceCounts[kind] ?? 0
  const healthy = provider.health.status === 'ok'
  const root = `/storage/providers/${encodeURIComponent(provider.id)}/linstor`
  const dataPools = (poolsQuery.data?.data ?? []).filter((pool) => pool.provider_kind !== 'DISKLESS' && Number(pool.total_capacity) > 0 && Number(pool.total_capacity) <= Number.MAX_SAFE_INTEGER)
  const totalCapacity = dataPools.reduce((sum, pool) => sum + Number(pool.total_capacity), 0)
  const freeCapacity = dataPools.reduce((sum, pool) => sum + Math.max(0, Number(pool.free_capacity) || 0), 0)
  const usedCapacity = Math.max(0, totalCapacity - freeCapacity)
  const diskStates = (resourcesQuery.data?.data ?? []).flatMap((resource) => resource.volumes ?? []).map((volume) => String(volume.state?.disk_state ?? 'Unknown'))
  const upToDate = diskStates.filter((value) => value.toLowerCase() === 'uptodate').length
  const needsAttention = diskStates.filter((value) => !['uptodate', 'unknown'].includes(value.toLowerCase())).length
  const unknown = diskStates.length - upToDate - needsAttention
  const summaryIssues = data?.conditions?.filter((condition) => condition.severity === 'error' || condition.severity === 'warning') ?? []
  return <div data-testid="linstor-provider-page">
    <PageHeader title="Dashboard" description="Piraeus lifecycle health, LINSTOR capacity and replication evidence, and Kubernetes ownership." actions={<Button variant="outline" onClick={() => void query.refetch()}><RefreshCw size={15}/> Refresh</Button>} />
    <QueryState isLoading={query.isLoading} error={query.error as Error | null} onRetry={() => void query.refetch()}>
      {data ? <div className="space-y-5">
        <section className={`rounded-xl border p-5 ${healthy ? 'border-[var(--color-success)]/30 bg-[var(--color-success)]/5' : 'border-[var(--color-warning)]/40 bg-[var(--color-warning)]/5'}`}>
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between"><div className="flex items-start gap-3">
            <div className={`rounded-full p-2.5 ${healthy ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]' : 'bg-[var(--color-warning)]/10 text-[var(--color-warning)]'}`}>{healthy ? <CheckCircle2 size={22}/> : <AlertTriangle size={22}/>}</div>
            <div><div className="flex flex-wrap items-center gap-2"><h2 className="text-xl font-semibold">{healthy ? 'Piraeus / LINSTOR is healthy' : 'Piraeus / LINSTOR needs attention'}</h2><Badge tone={tone(provider.health.status)}>{provider.health.status}</Badge><Badge tone="info">externally managed</Badge></div>
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">Highland observes this deployment but never owns its install or availability. Uninstalling Highland does not affect CSI or stored data.</p></div></div>
            <div className="flex gap-2"><Link className="inline-flex h-9 items-center gap-2 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-[var(--color-primary-foreground)]" to={`${root}/storage-pools`}><Database size={15}/> Capacity</Link><Link className="inline-flex h-9 items-center gap-2 rounded-md border border-[var(--color-border)] px-4 text-sm font-medium" to={`/storage/providers/${encodeURIComponent(provider.id)}/context`}><GitBranch size={15}/> Context</Link></div>
          </div>
        </section>
        {summaryIssues.map((condition) => <Alert key={`${condition.type}:${condition.reason}`} tone={condition.severity === 'error' ? 'danger' : 'warning'}><AlertTitle>{condition.type}</AlertTitle><AlertDescription>{condition.message}</AlertDescription></Alert>)}
        <section aria-labelledby="linstor-operational-signals-heading">
          <div className="mb-3"><h2 id="linstor-operational-signals-heading" className="text-base font-semibold">Operational signals</h2><p className="text-sm text-[var(--color-muted-foreground)]">Readiness, topology, resource placement, and protection coverage.</p></div>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric icon={Boxes} label="Component readiness" value={`${ready}/${data.components.length}`} detail="Kubernetes workloads ready" warning={ready !== data.components.length}/>
            <Metric icon={Server} label="Nodes" value={String(count('nodes'))} detail={`${count('satellites')} operator satellites`}/>
            <Metric icon={HardDrive} label="Resources" value={String(count('resource-definitions'))} detail={`${count('resources')} placed replica records`}/>
            <Metric icon={Network} label="Protection" value={`${count('snapshots')} snapshots`} detail={`${count('remotes')} remotes · ${count('schedules')} schedules`}/>
          </div>
        </section>
        <section aria-labelledby="linstor-capacity-resilience-heading">
          <div className="mb-3"><h2 id="linstor-capacity-resilience-heading" className="text-base font-semibold">Capacity & resilience</h2><p className="text-sm text-[var(--color-muted-foreground)]">Usable diskful capacity and the current safety state of placed replicas.</p></div>
        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader><CardTitle>Allocatable pool capacity</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              {poolsQuery.isError ? <p className="text-sm text-[var(--color-warning)]">Pool capacity is temporarily unavailable.</p> : poolsQuery.isLoading ? <p className="text-sm text-[var(--color-muted-foreground)]">Loading controller capacity…</p> : totalCapacity > 0 ? <>
                <div className="flex items-end justify-between gap-4"><div><div className="text-2xl font-semibold">{formatCapacity(usedCapacity)}</div><p className="text-xs text-[var(--color-muted-foreground)]">used across {dataPools.length} allocatable pool{dataPools.length === 1 ? '' : 's'}</p></div><div className="text-right text-sm"><div className="font-medium">{formatCapacity(freeCapacity)} free</div><div className="text-xs text-[var(--color-muted-foreground)]">{formatCapacity(totalCapacity)} total</div></div></div>
                <UsageBar used={usedCapacity} total={totalCapacity} />
                <p className="text-xs text-[var(--color-muted-foreground)]">Diskless pools are excluded because they place replicas but do not contribute storage capacity.</p>
              </> : <p className="text-sm text-[var(--color-muted-foreground)]">No allocatable diskful pool capacity was reported.</p>}
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>Replica disk state</CardTitle></CardHeader>
            <CardContent className="flex flex-col gap-5 sm:flex-row sm:items-center">
              <Donut slices={[
                { label: 'Up to date', value: upToDate, color: 'var(--color-success)' },
                { label: 'Needs attention', value: needsAttention, color: 'var(--color-destructive)' },
                { label: 'Unknown', value: unknown, color: 'var(--color-warning)' },
              ]} />
              <div className="min-w-0 flex-1 space-y-2">
                <LegendRow color="var(--color-success)" label="Up to date" value={upToDate} />
                <LegendRow color="var(--color-destructive)" label="Needs attention" value={needsAttention} />
                <LegendRow color="var(--color-warning)" label="Unknown" value={unknown} />
                <p className="pt-2 text-xs text-[var(--color-muted-foreground)]">Current disk state of controller-observed resource replicas. A healthy protected resource should have every intended replica UpToDate.</p>
              </div>
            </CardContent>
          </Card>
        </div>
        </section>
        <ProviderWorkloadFootprint provider={provider.id} />
        <section aria-labelledby="linstor-provider-resources-heading">
          <div className="mb-3"><h2 id="linstor-provider-resources-heading" className="text-base font-semibold">Provider resources</h2><p className="text-sm text-[var(--color-muted-foreground)]">Continue into Piraeus lifecycle, LINSTOR placement, protection, and diagnostics.</p></div>
          <div className="grid gap-4 lg:grid-cols-3">
            <Area title="Lifecycle" icon={Activity} text="Operator convergence and workload readiness." links={[["Components",`${root}/components`],["Clusters",`${root}/clusters`],["Satellites",`${root}/satellites`]]}/>
            <Area title="Capacity & placement" icon={Database} text="Nodes, pools, policy, and replica placement." links={[["Nodes",`${root}/nodes`],["Storage pools",`${root}/storage-pools`],["Resource groups",`${root}/resource-groups`],["Replicas",`${root}/resources`]]}/>
            <Area title="Protection & diagnostics" icon={Network} text="Snapshots, backup destinations, schedules, and errors." links={[["Snapshots",`${root}/snapshots`],["Remotes",`${root}/remotes`],["Schedules",`${root}/schedules`],["Error reports",`${root}/error-reports`]]}/>
          </div>
        </section>
        {provider.health.conditions.length ? <Card><CardHeader><CardTitle>Health evidence</CardTitle></CardHeader><CardContent className="space-y-2">{provider.health.conditions.map((condition) => <div key={`${condition.type}:${condition.reason}`} className="flex items-start justify-between gap-3 rounded-md border border-[var(--color-border)] p-3"><div><div className="font-medium">{condition.type}</div><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{condition.message}</p></div><Badge tone={tone(condition.severity)}>{condition.status}</Badge></div>)}</CardContent></Card> : null}
      </div> : null}
    </QueryState>
  </div>
}

function Metric({icon:Icon,label,value,detail,warning}:{icon:typeof Boxes;label:string;value:string;detail:string;warning?:boolean}) { return <Card><CardContent className="pt-5"><div className="flex items-center gap-2 text-xs text-[var(--color-muted-foreground)]"><Icon size={15}/>{label}</div><div className={`mt-2 text-2xl font-semibold ${warning?'text-[var(--color-warning)]':''}`}>{value}</div><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{detail}</p></CardContent></Card> }
function Area({title,icon:Icon,text,links}:{title:string;icon:typeof Boxes;text:string;links:string[][]}) { return <Card><CardHeader><CardTitle className="flex items-center gap-2"><Icon size={17}/>{title}</CardTitle></CardHeader><CardContent><p className="mb-3 text-sm text-[var(--color-muted-foreground)]">{text}</p><div className="flex flex-wrap gap-2">{links.map(([label,to])=><Link key={to} className="rounded-md border border-[var(--color-border)] px-2.5 py-1.5 text-xs font-medium hover:border-[var(--color-primary)]" to={to!}>{label}</Link>)}</div></CardContent></Card> }

function useFilters() { const [params,setParams]=useSearchParams(); const value:StorageFilters={search:params.get('search')||undefined,limit:100}; return {value,setSearch:(search:string)=>{const next=new URLSearchParams(params);if(search)next.set('search',search);else next.delete('search');setParams(next)}} }
function columns(provider:string,kind:string):ColumnDef<Resource,unknown>[] { return [
  {id:'name',header:'Resource',cell:({row})=>{const id=String(row.original.id??row.original.name??'');return <Link className="font-medium text-[var(--color-primary)] hover:underline" to={`/storage/providers/${encodeURIComponent(provider)}/linstor/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`}>{String(row.original.name??id)}</Link>}},
  {id:'state',header:'State',cell:({row})=><Badge tone={tone(state(row.original))}>{state(row.original)}</Badge>},
  {id:'node',header:'Node',accessorFn:(row)=>String(row.node_name??row.node??'—')},
  {id:'free',header:'Free capacity',accessorFn:(row)=>formatCapacity(row.free_capacity,row.provider_kind==='DISKLESS')},
  {id:'source',header:'Source',accessorFn:(row)=>String(row.source==='linstor-rest'?'LINSTOR controller':row.source==='piraeus-crd'?'Piraeus CRD':'Kubernetes workload')},
] }

export function LinstorResourcePage() { const {providerId='',kind=''}=useParams();const stateFilters=useFilters();const query=useProviderResources<Resource>(providerId,kind,stateFilters.value);const copy=config[kind]??{title:kind.replaceAll('-',' '),description:'LINSTOR resources.',guidance:'Inspect state and source freshness before acting in native tooling.'};const table=useMemo(()=>columns(providerId,kind),[providerId,kind]);const rows=query.data?.data??[];return <div data-testid="linstor-resource-page"><PageHeader title={copy.title} description={copy.description} actions={<Badge tone="info">read only</Badge>}/><div className="mb-4 flex flex-col gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 lg:flex-row lg:items-center lg:justify-between"><div><p className="text-sm font-medium">What good looks like</p><p className="mt-1 max-w-3xl text-xs text-[var(--color-muted-foreground)]">{copy.guidance}</p></div><div className="flex gap-2"><Input className="lg:w-64" aria-label={`Search ${copy.title}`} placeholder="Search resources" value={stateFilters.value.search??''} onChange={(event)=>stateFilters.setSearch(event.target.value)}/><Button variant="outline" size="icon" aria-label="Refresh" onClick={()=>void query.refetch()}><RefreshCw size={15}/></Button></div></div><QueryState isLoading={query.isLoading} error={query.error as Error|null} onRetry={()=>void query.refetch()}>{rows.length?<DataTable columns={table} data={rows} tableId={`linstor-${kind}`} getRowId={(row,index)=>String(row.id??row.name??index)} enableExport exportName={`linstor-${kind}`}/>:<Card><CardContent className="py-12 text-center"><Database className="mx-auto text-[var(--color-muted-foreground)]"/><p className="mt-3 text-sm font-medium">No {copy.title.toLowerCase()} observed</p><p className="mt-1 text-xs text-[var(--color-muted-foreground)]">The source may be unconfigured, unavailable, or have no resources yet.</p></CardContent></Card>}</QueryState></div> }

function flatten(value:unknown,prefix='',depth=0):Array<[string,string]>{if(depth>4||value==null)return[];if(Array.isArray(value))return prefix?[[prefix,value.slice(0,20).map(String).join(', ')]]:[];if(typeof value!=='object')return prefix?[[prefix,String(value)]]:[];const out:Array<[string,string]>=[];for(const [key,child] of Object.entries(value as Record<string,unknown>)){if(/secret|password|credential|token|chap|key$/i.test(key))continue;const path=prefix?`${prefix}.${key}`:key;if(child!==null&&typeof child==='object')out.push(...flatten(child,path,depth+1));else out.push([path,String(child??'—')]);if(out.length>=60)break}return out.slice(0,60)}
export function LinstorResourceDetailPage(){const {providerId='',kind='',resourceId=''}=useParams();const id=decodeURIComponent(resourceId);const query=useProviderResource<Resource>(providerId,kind,id);const rows=useMemo(()=>flatten(query.data),[query.data]);const copy=config[kind]??{title:kind.replaceAll('-',' ')};return <div><PageHeader title={String(query.data?.name??id)} description={`${copy.title} detail from an authoritative, bounded source.`} actions={<Badge tone="info">read only</Badge>}/><QueryState isLoading={query.isLoading} error={query.error as Error|null} onRetry={()=>void query.refetch()}>{query.data?<div className="space-y-4"><Alert><AlertTitle>Externally managed</AlertTitle><AlertDescription>Use Piraeus or LINSTOR native workflows to change this resource. Highland remains available for inventory, correlation, and impact analysis.</AlertDescription></Alert><Card><CardHeader><CardTitle>Configuration and runtime details</CardTitle></CardHeader><CardContent><dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">{rows.map(([label,value])=><div key={label} className="min-w-0 border-b border-[var(--color-border)] pb-2"><dt className="text-xs capitalize text-[var(--color-muted-foreground)]">{label.replaceAll('_',' ').replaceAll('.',' › ')}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div>)}</dl></CardContent></Card></div>:null}</QueryState></div>}
