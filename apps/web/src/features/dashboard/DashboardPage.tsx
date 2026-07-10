import { Activity, HardDrive, Server, TriangleAlert } from 'lucide-react'
import {
  useCapacity,
  useDashboard,
  useEvents,
  useHealthNarrative,
  useNodes,
  useVolumes,
} from '@/api/hooks'
import { formatBytes } from '@/api/longhorn'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

function StatCard({
  title,
  value,
  hint,
  icon: Icon,
}: {
  title: string
  value: string | number
  hint?: string
  icon: typeof Server
}) {
  return (
    <Card>
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
}

export function DashboardPage() {
  const { t } = useAppTranslation()
  const dash = useDashboard()
  const volumes = useVolumes()
  const nodes = useNodes()
  const events = useEvents()
  const health = useHealthNarrative()
  const capacity = useCapacity()

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
          />
          <StatCard
            title={t('dashboard.attached')}
            value={attached}
            hint={t('dashboard.attachedHint', { count: volList.length - attached })}
            icon={Activity}
          />
          <StatCard
            title={t('dashboard.nodes')}
            value={nodeList.length}
            hint={t('dashboard.nodesHint', { count: schedulable })}
            icon={Server}
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
          />
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
