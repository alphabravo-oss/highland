import { useMemo } from 'react'
import { ArrowLeft, Cpu, HardDrive, MemoryStick } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useClusterMetrics, useEngineImages, useInstanceManagers, useNode } from '@/api/hooks'
import { formatBytes, toConditionArray, type EngineImage, type Node } from '@/api/longhorn'
import { MetricLine, UsageBar } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type MetricPoint = { t: string; v: number }
type Series = { name: string; labels?: Record<string, string>; points: MetricPoint[] }

// Fields Longhorn returns that aren't on the shared Disk/Node types.
type DiskEx = NonNullable<Node['disks']>[string] & {
  diskUUID?: string
  diskDriver?: string
  evictionRequested?: boolean
  scheduledReplica?: Record<string, number>
  healthData?: Record<string, { attributes?: Array<{ name?: string; value?: string; rawValue?: string }> }>
}
type NodeEx = Node & {
  address?: string
  autoEvicting?: boolean
  evictionRequested?: boolean
}
// nodeDeploymentMap maps nodeID -> deployed(bool); not on the shared type.
type EngineImageEx = EngineImage & {
  nodeDeploymentMap?: Record<string, boolean>
}

function latest(series: Series[], name: string, node: string): number | undefined {
  const s = series.find((x) => x.name === name && (x.labels?.node === node || !x.labels?.node))
  return s?.points.at(-1)?.v
}

/** Sum matching volume-throughput series for one node, point-wise. */
function nodeThroughput(series: Series[], node: string, match: string): number[] {
  const rel = series.filter((s) => s.name.includes(match) && s.labels?.node === node)
  const len = Math.max(0, ...rel.map((s) => s.points.length))
  const out = new Array(len).fill(0)
  for (const s of rel) {
    const off = len - s.points.length
    s.points.forEach((p, i) => (out[off + i] += p.v))
  }
  return out
}

function UsageCard({
  title,
  icon: Icon,
  used,
  total,
  fmt,
  extra,
}: {
  title: string
  icon: typeof Cpu
  used?: number
  total?: number
  fmt: (v: number) => string
  extra?: React.ReactNode
}) {
  const pct = used != null && total ? Math.round((used / total) * 100) : undefined
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0 border-0 pb-0 pt-4">
        <CardTitle className="text-[var(--color-muted-foreground)]">{title}</CardTitle>
        <Icon size={18} strokeWidth={1.75} className="text-[var(--color-primary)]" />
      </CardHeader>
      <CardContent className="space-y-2">
        <div className="text-2xl font-semibold tabular-nums">{pct != null ? `${pct}%` : '—'}</div>
        {used != null && total ? <UsageBar used={used} total={total} /> : null}
        <p className="text-xs text-[var(--color-muted-foreground)]">
          {used != null && total != null ? `${fmt(used)} / ${fmt(total)}` : '—'}
        </p>
        {extra}
      </CardContent>
    </Card>
  )
}

export function NodeDetailPage() {
  const { t } = useAppTranslation()
  const { name } = useParams<{ name: string }>()
  const q = useNode(name)
  const metrics = useClusterMetrics()
  const instanceManagersQ = useInstanceManagers()
  const engineImagesQ = useEngineImages()
  const node = q.data as NodeEx | undefined
  const series = (metrics.data?.series ?? []) as Series[]

  const nodeInstanceManagers = useMemo(
    () => (instanceManagersQ.data ?? []).filter((im) => im.nodeID === name),
    [instanceManagersQ.data, name],
  )
  const nodeEngineImages = useMemo(() => {
    const imgs = (engineImagesQ.data ?? []) as EngineImageEx[]
    return imgs.filter((img) => {
      const map = img.nodeDeploymentMap
      // If the API reports per-node deployment, only show images on this node;
      // otherwise fall back to showing all cluster engine images.
      return map ? Boolean(name && map[name]) : true
    })
  }, [engineImagesQ.data, name])

  const cpuU = latest(series, 'longhorn_node_cpu_usage_millicpu', name ?? '')
  const cpuC = latest(series, 'longhorn_node_cpu_capacity_millicpu', name ?? '')
  const memU = latest(series, 'longhorn_node_memory_usage_bytes', name ?? '')
  const memC = latest(series, 'longhorn_node_memory_capacity_bytes', name ?? '')
  const stoU = latest(series, 'longhorn_node_storage_usage_bytes', name ?? '')
  const stoC = latest(series, 'longhorn_node_storage_capacity_bytes', name ?? '')
  const stoR = latest(series, 'longhorn_node_storage_reservation_bytes', name ?? '')
  const stoS = latest(series, 'longhorn_node_storage_scheduled_bytes', name ?? '')

  const writeAgg = useMemo(() => nodeThroughput(series, name ?? '', 'write_throughput'), [series, name])
  const readAgg = useMemo(() => nodeThroughput(series, name ?? '', 'read_throughput'), [series, name])

  const conditions = toConditionArray(node?.conditions)
  const disks = Object.entries((node?.disks ?? {}) as Record<string, DiskEx>)
  const ready = conditions.find((c) => c.type === 'Ready')

  return (
    <div data-testid="node-detail-page">
      <Link
        to="/nodes"
        className="mb-1 inline-flex items-center gap-1 text-sm text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
      >
        <ArrowLeft size={14} /> {t('nodes.title')}
      </Link>
      <PageHeader
        title={name ?? ''}
        description={node?.address ? `${t('nodeDetail.address')}: ${node.address}` : undefined}
      />
      <QueryState isLoading={q.isLoading} error={q.error as Error | null} onRetry={() => void q.refetch()}>
        {/* status row */}
        <div className="mb-4 flex flex-wrap items-center gap-2 text-sm">
          <Badge tone={ready?.status === 'True' ? 'success' : 'danger'}>
            {ready?.status === 'True' ? t('nodeDetail.ready') : t('nodeDetail.notReady')}
          </Badge>
          <Badge tone={node?.allowScheduling ? 'success' : 'warning'}>
            {node?.allowScheduling ? t('nodeDetail.schedulable') : t('nodeDetail.unschedulable')}
          </Badge>
          {node?.evictionRequested ? <Badge tone="danger">{t('nodeDetail.evicting')}</Badge> : null}
          {node?.zone ? <Badge tone="info">{t('nodeDetail.zone')}: {node.zone}</Badge> : null}
          {node?.region ? <Badge tone="info">{t('nodeDetail.region')}: {node.region}</Badge> : null}
          {(node?.tags ?? []).map((tag) => (
            <Badge key={tag} tone="default">{tag}</Badge>
          ))}
        </div>

        {/* usage gauges */}
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <UsageCard title={t('nodeDetail.cpu')} icon={Cpu} used={cpuU} total={cpuC} fmt={(v) => `${(v / 1000).toFixed(2)} ${t('nodeDetail.cores')}`} />
          <UsageCard title={t('nodeDetail.memory')} icon={MemoryStick} used={memU} total={memC} fmt={formatBytes} />
          <UsageCard
            title={t('nodeDetail.storage')}
            icon={HardDrive}
            used={stoU}
            total={stoC}
            fmt={formatBytes}
            extra={
              <p className="text-xs text-[var(--color-muted-foreground)]">
                {t('nodeDetail.reserved')} {formatBytes(stoR)} · {t('nodeDetail.scheduled')} {formatBytes(stoS)}
              </p>
            }
          />
        </div>

        {/* node throughput */}
        <Card className="mt-4">
          <CardHeader>
            <CardTitle>{t('nodeDetail.io')}</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <MetricLine
              label={t('nodeDetail.writeThroughput')}
              points={writeAgg}
              format={(v) => `${formatBytes(v)}/s`}
              emptyLabel={t('nodeDetail.noIo')}
              peakLabel={t('common.peak')}
            axis
            />
            <MetricLine
              label={t('nodeDetail.readThroughput')}
              points={readAgg}
              format={(v) => `${formatBytes(v)}/s`}
              emptyLabel={t('nodeDetail.noIo')}
              peakLabel={t('common.peak')}
            axis
            />
          </CardContent>
        </Card>

        {/* conditions */}
        <Card className="mt-4">
          <CardHeader>
            <CardTitle>{t('nodeDetail.conditions')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {conditions.length === 0 ? (
              <p className="text-sm text-[var(--color-muted-foreground)]">—</p>
            ) : (
              conditions.map((c, i) => (
                <div key={i} className="flex items-start gap-2 text-sm">
                  <Badge tone={c.status === 'True' ? 'success' : 'warning'}>{c.type}</Badge>
                  {c.message ? (
                    <span className="text-[var(--color-muted-foreground)]">{c.message}</span>
                  ) : (
                    <span className="text-[var(--color-muted-foreground)]">{c.status}</span>
                  )}
                </div>
              ))
            )}
          </CardContent>
        </Card>

        {/* node components / readiness */}
        <Card className="mt-4">
          <CardHeader>
            <CardTitle>{t('nodeDetail.components')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <p className="mb-1 text-xs font-medium text-[var(--color-muted-foreground)]">
                {t('nodeDetail.instanceManagers')}
              </p>
              {nodeInstanceManagers.length === 0 ? (
                <p className="text-sm text-[var(--color-muted-foreground)]">{t('common.none')}</p>
              ) : (
                <Table>
                  <THead>
                    <TR>
                      <TH>{t('common.name')}</TH>
                      <TH>{t('common.type')}</TH>
                      <TH>{t('common.state')}</TH>
                      <TH>{t('common.image')}</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {nodeInstanceManagers.map((im) => (
                      <TR key={im.name}>
                        <TD className="max-w-xs truncate font-mono text-xs">{im.name}</TD>
                        <TD>{im.instanceManagerType ?? '—'}</TD>
                        <TD>
                          <Badge tone={stateTone(im.currentState ?? '')}>
                            {im.currentState ?? t('common.unknown')}
                          </Badge>
                        </TD>
                        <TD className="max-w-xs truncate font-mono text-xs" title={im.image}>
                          {im.image ?? '—'}
                        </TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
            <div>
              <p className="mb-1 text-xs font-medium text-[var(--color-muted-foreground)]">
                {t('nodeDetail.engineImages')}
              </p>
              {nodeEngineImages.length === 0 ? (
                <p className="text-sm text-[var(--color-muted-foreground)]">{t('common.none')}</p>
              ) : (
                <Table>
                  <THead>
                    <TR>
                      <TH>{t('common.name')}</TH>
                      <TH>{t('common.state')}</TH>
                      <TH>{t('common.refs')}</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {nodeEngineImages.map((img) => (
                      <TR key={img.name}>
                        <TD className="max-w-xs truncate font-mono text-xs" title={img.image ?? img.name}>
                          {img.image ?? img.name}
                        </TD>
                        <TD>
                          <Badge tone={stateTone(img.state ?? '')}>
                            {img.state ?? t('common.unknown')}
                          </Badge>
                        </TD>
                        <TD className="tabular-nums">{img.refCount ?? 0}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
          </CardContent>
        </Card>

        {/* per-disk detail */}
        <h2 className="mb-2 mt-6 text-sm font-semibold tracking-tight">{t('nodeDetail.disks')}</h2>
        <div className="space-y-3">
          {disks.map(([id, d]) => {
            const used = (d.storageMaximum ?? 0) - (d.storageAvailable ?? 0)
            const diskConds = toConditionArray(d.conditions)
            const diskReady = diskConds.find((c) => c.type === 'Ready')
            const scheduled = Object.entries(d.scheduledReplica ?? {})
            const smart = Object.values(d.healthData ?? {})[0]?.attributes ?? []
            const crit = smart.find((a) => /CriticalWarning/i.test(a.name ?? ''))
            return (
              <Card key={id}>
                <CardHeader className="flex-row items-center justify-between space-y-0">
                  <CardTitle className="font-mono text-xs">{d.path ?? id}</CardTitle>
                  <div className="flex gap-1">
                    <Badge tone="default">{d.diskType ?? 'filesystem'}</Badge>
                    <Badge tone={diskReady?.status === 'True' ? 'success' : 'warning'}>
                      {diskReady?.status === 'True' ? t('nodeDetail.ready') : (diskReady?.type ?? t('nodeDetail.notReady'))}
                    </Badge>
                    <Badge tone={d.allowScheduling ? 'success' : 'warning'}>
                      {d.allowScheduling ? t('nodeDetail.schedulable') : t('nodeDetail.unschedulable')}
                    </Badge>
                  </div>
                </CardHeader>
                <CardContent className="space-y-3">
                  <UsageBar used={used} total={d.storageMaximum ?? 0} />
                  <div className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm sm:grid-cols-4">
                    <Meta label={t('nodes.maximum')} value={formatBytes(d.storageMaximum)} />
                    <Meta label={t('nodes.available')} value={formatBytes(d.storageAvailable)} />
                    <Meta label={t('nodeDetail.reserved')} value={formatBytes(d.storageReserved)} />
                    <Meta label={t('nodes.scheduled')} value={formatBytes(d.storageScheduled)} />
                    <Meta label={t('nodeDetail.diskUuid')} value={d.diskUUID ?? '—'} mono />
                    <Meta label={t('nodeDetail.diskDriver')} value={d.diskDriver || t('nodeDetail.auto')} />
                    <Meta label={t('nodeDetail.smart')} value={crit ? `${t('nodeDetail.critWarn')}: ${crit.rawValue ?? crit.value}` : (smart.length ? t('nodeDetail.smartOk') : '—')} />
                    <Meta label={t('nodes.tags')} value={(d.tags ?? []).join(', ') || '—'} />
                  </div>
                  {scheduled.length > 0 ? (
                    <div>
                      <p className="mb-1 text-xs font-medium text-[var(--color-muted-foreground)]">
                        {t('nodeDetail.scheduledReplicas', { count: scheduled.length })}
                      </p>
                      <Table>
                        <THead>
                          <TR>
                            <TH>{t('nodeDetail.replica')}</TH>
                            <TH>{t('nodeDetail.size')}</TH>
                          </TR>
                        </THead>
                        <TBody>
                          {scheduled.map(([rname, size]) => (
                            <TR key={rname}>
                              <TD className="max-w-xs truncate font-mono text-xs">{rname}</TD>
                              <TD className="tabular-nums">{formatBytes(size)}</TD>
                            </TR>
                          ))}
                        </TBody>
                      </Table>
                    </div>
                  ) : null}
                </CardContent>
              </Card>
            )
          })}
        </div>
      </QueryState>
    </div>
  )
}

function Meta({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="text-xs text-[var(--color-muted-foreground)]">{label}</div>
      <div className={mono ? 'truncate font-mono text-xs' : 'tabular-nums'} title={value}>
        {value}
      </div>
    </div>
  )
}
