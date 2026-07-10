import { Activity, HardDrive, Server, TriangleAlert } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  useCapacity,
  useClusterMetrics,
  useDashboard,
  useEvents,
  useHealthNarrative,
  useNodes,
  useVolumes,
} from '@/api/hooks'
import { formatBytes } from '@/api/longhorn'
import { Donut, LegendRow, MetricLine, UsageBar } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const ROBUSTNESS_COLORS = {
  healthy: '#16a34a',
  degraded: '#d97706',
  faulted: '#dc2626',
} as const

function StatCard({
  title,
  value,
  hint,
  icon: Icon,
  to,
}: {
  title: string
  value: string | number
  hint?: string
  icon: typeof Server
  to?: string
}) {
  const inner = (
    <Card
      className={
        to
          ? 'h-full transition-colors hover:border-[var(--color-primary)] hover:bg-[var(--color-accent,rgba(120,120,120,0.06))]'
          : 'h-full'
      }
    >
      <CardHeader className="flex-row items-center justify-between space-y-0 border-0 pb-0 pt-4">
        <CardTitle className="text-[var(--color-muted-foreground)]">{title}</CardTitle>
        <Icon size={18} strokeWidth={1.75} className="text-[var(--color-primary)]" />
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-semibold tabular-nums">{value}</div>
        {hint ? <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{hint}</p> : null}
      </CardContent>
    </Card>
  )
  return to ? (
    <Link to={to} className="block focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-primary)] rounded-lg">
      {inner}
    </Link>
  ) : (
    inner
  )
}

/** Sum matching throughput series point-wise into one cluster-wide series. */
function aggregateSeries(
  series: Array<{ name: string; points: Array<{ v: number }> }>,
  match: string,
): number[] {
  const relevant = series.filter((s) => s.name.includes(match))
  const len = Math.max(0, ...relevant.map((s) => s.points.length))
  const out = new Array(len).fill(0)
  for (const s of relevant) {
    const offset = len - s.points.length
    s.points.forEach((p, i) => {
      out[offset + i] += p.v
    })
  }
  return out
}

export function DashboardPage() {
  const { t } = useAppTranslation()
  const dash = useDashboard()
  const volumes = useVolumes()
  const nodes = useNodes()
  const events = useEvents()
  const health = useHealthNarrative()
  const capacity = useCapacity()
  const metrics = useClusterMetrics()

  const volList = volumes.data ?? []
  const nodeList = nodes.data ?? []
  const healthy = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'healthy').length
  const degraded = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'degraded').length
  const faulted = volList.filter((v) => (v.robustness ?? '').toLowerCase() === 'faulted').length
  const attached = volList.filter((v) => (v.state ?? '').toLowerCase() === 'attached').length
  const schedulable = nodeList.filter((n) => n.allowScheduling).length

  // Prefer live collection counts; overlay dashboard API if present
  const d = dash.data
  const storageTotal =
    typeof d?.storage === 'object' && d.storage && 'total' in d.storage
      ? Number((d.storage as { total?: number }).total)
      : undefined

  const metricSeries = metrics.data?.series ?? []
  const writeAgg = aggregateSeries(metricSeries, 'write_throughput')
  const readAgg = aggregateSeries(metricSeries, 'read_throughput')

  const loading = volumes.isLoading || nodes.isLoading
  const error =
    (volumes.error as Error | null) ??
    (nodes.error as Error | null) ??
    (dash.error as Error | null)

  return (
    <div data-testid="dashboard-page">
      <PageHeader
        title={t('dashboard.title')}
        description={t('dashboard.description')}
      />
      <QueryState
        isLoading={loading}
        error={error}
        onRetry={() => {
          void volumes.refetch()
          void nodes.refetch()
          void dash.refetch()
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <StatCard
            title={t('dashboard.volumes')}
            value={volList.length}
            hint={t('dashboard.volumeHint', { healthy, degraded, faulted })}
            icon={HardDrive}
            to="/volumes"
          />
          <StatCard
            title={t('dashboard.attached')}
            value={attached}
            hint={t('dashboard.attachedHint', { count: volList.length - attached })}
            icon={Activity}
            to="/volumes"
          />
          <StatCard
            title={t('dashboard.nodes')}
            value={nodeList.length}
            hint={t('dashboard.nodesHint', { count: schedulable })}
            icon={Server}
            to="/nodes"
          />
          <StatCard
            title={t('dashboard.storage')}
            value={
              storageTotal != null && storageTotal > 0
                ? formatBytes(storageTotal)
                : capacity.data?.totalBytes
                  ? formatBytes(capacity.data.totalBytes)
                  : '—'
            }
            hint={
              capacity.data?.note
                ? t('dashboard.storageHintUsed', {
                    used: formatBytes(capacity.data.usedBytes),
                    series: capacity.data.seriesCount,
                  })
                : t('dashboard.storageHintDefault')
            }
            icon={TriangleAlert}
            to="/nodes"
          />
        </div>

        <div className="mt-4 grid gap-3 lg:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle>{t('dashboard.robustness')}</CardTitle>
            </CardHeader>
            <CardContent className="flex items-center gap-4">
              <Donut
                slices={[
                  { label: t('dashboard.healthy'), value: healthy, color: ROBUSTNESS_COLORS.healthy },
                  { label: t('dashboard.degraded'), value: degraded, color: ROBUSTNESS_COLORS.degraded },
                  { label: t('dashboard.faulted'), value: faulted, color: ROBUSTNESS_COLORS.faulted },
                ]}
              />
              <div className="flex-1 space-y-1.5">
                <LegendRow color={ROBUSTNESS_COLORS.healthy} label={t('dashboard.healthy')} value={healthy} />
                <LegendRow color={ROBUSTNESS_COLORS.degraded} label={t('dashboard.degraded')} value={degraded} />
                <LegendRow color={ROBUSTNESS_COLORS.faulted} label={t('dashboard.faulted')} value={faulted} />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('dashboard.capacity')}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {capacity.data && capacity.data.totalBytes > 0 ? (
                <>
                  <UsageBar used={capacity.data.usedBytes} total={capacity.data.totalBytes} />
                  <div className="flex justify-between text-sm">
                    <span className="text-[var(--color-muted-foreground)]">
                      {t('dashboard.capacityUsed', {
                        used: formatBytes(capacity.data.usedBytes),
                        total: formatBytes(capacity.data.totalBytes),
                      })}
                    </span>
                    <span className="tabular-nums font-medium">
                      {Math.round((capacity.data.usedBytes / capacity.data.totalBytes) * 100)}%
                    </span>
                  </div>
                </>
              ) : (
                <p className="text-sm text-[var(--color-muted-foreground)]">{t('dashboard.capacityUnavailable')}</p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex-row items-center justify-between space-y-0">
              <CardTitle>{t('dashboard.clusterIo')}</CardTitle>
              <Link to="/performance" className="text-xs text-[var(--color-primary)] hover:underline">
                {t('dashboard.liveIo')}
              </Link>
            </CardHeader>
            <CardContent className="space-y-3">
              <MetricLine
                label={t('dashboard.writeThroughput')}
                points={writeAgg}
                format={(v) => `${formatBytes(v)}/s`}
                emptyLabel={t('dashboard.noIo')}
                peakLabel={t('common.peak')}
              />
              <MetricLine
                label={t('dashboard.readThroughput')}
                points={readAgg}
                format={(v) => `${formatBytes(v)}/s`}
                emptyLabel={t('dashboard.noIo')}
                peakLabel={t('common.peak')}
              />
            </CardContent>
          </Card>
        </div>

        {(health.data?.items ?? []).length > 0 ? (
          <Card className="mt-4">
            <CardHeader>
              <CardTitle>{t('dashboard.healthNarrative')}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              {health.data!.items.map((item, i) => (
                <div key={i} className="flex items-start gap-2 text-sm">
                  <Badge
                    tone={
                      item.severity === 'ok'
                        ? 'success'
                        : item.severity === 'warning'
                          ? 'warning'
                          : 'info'
                    }
                  >
                    {item.severity}
                  </Badge>
                  <span>
                    <span className="font-mono text-xs text-[var(--color-muted-foreground)]">
                      {item.code}
                    </span>{' '}
                    {item.message}
                  </span>
                </div>
              ))}
            </CardContent>
          </Card>
        ) : null}

        <div className="mt-6 grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>{t('dashboard.volumeHealth')}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              {volList.length === 0 ? (
                <p className="text-sm text-[var(--color-muted-foreground)]">{t('dashboard.noVolumesYet')}</p>
              ) : (
                volList.slice(0, 8).map((v) => (
                  <div key={v.id ?? v.name} className="flex items-center justify-between gap-2 text-sm">
                    <span className="truncate font-medium">{v.name}</span>
                    <div className="flex gap-1">
                      <Badge tone={stateTone(v.state)}>{v.state ?? '—'}</Badge>
                      <Badge tone={stateTone(v.robustness)}>{v.robustness ?? '—'}</Badge>
                    </div>
                  </div>
                ))
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('dashboard.recentEvents')}</CardTitle>
            </CardHeader>
            <CardContent>
              <QueryState
                isLoading={events.isLoading}
                error={events.error as Error | null}
                isEmpty={!events.data?.length}
                emptyTitle={t('dashboard.noEvents')}
                emptyDescription={t('dashboard.noEventsDescription')}
              >
                <Table>
                  <THead>
                    <TR>
                      <TH>{t('common.reason')}</TH>
                      <TH>{t('common.object')}</TH>
                      <TH>{t('common.message')}</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {(events.data ?? []).slice(0, 10).map((e) => (
                      <TR key={e.id}>
                        <TD className="whitespace-nowrap">{e.reason ?? e.eventType ?? '—'}</TD>
                        <TD className="whitespace-nowrap">
                          {e.involvedObject?.kind}/{e.involvedObject?.name}
                        </TD>
                        <TD className="max-w-xs truncate">{e.message ?? '—'}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              </QueryState>
            </CardContent>
          </Card>
        </div>
      </QueryState>
    </div>
  )
}
