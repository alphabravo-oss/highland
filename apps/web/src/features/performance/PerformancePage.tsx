import { useState } from 'react'
import { Activity, Gauge } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  useBenchmarks,
  useClusterMetrics,
  useCreateBenchmark,
  useDeleteBenchmark,
  useNodes,
  useVolumes,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { formatBytes } from '@/api/longhorn'
import { AreaSparkline } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'

/**
 * A labelled metric line: shows what is being measured, the current value and
 * peak (with units), and a trend chart with a hover tooltip.
 */
function MetricLine({
  label,
  points,
  format,
  emptyLabel,
}: {
  label: string
  points: Array<{ v: number }>
  format: (v: number) => string
  emptyLabel: string
}) {
  const vals = points.map((p) => p.v)
  const current = vals.at(-1) ?? 0
  const peak = vals.length ? Math.max(...vals) : 0
  const { t } = useAppTranslation()
  return (
    <div>
      <div className="mb-0.5 flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium text-[var(--color-foreground)]">{label}</span>
        <span className="tabular-nums text-sm font-semibold text-[var(--color-foreground)]">
          {format(current)}
        </span>
      </div>
      <div className="mb-1 text-[10px] text-[var(--color-muted-foreground)]">
        {t('performance.peak')}: <span className="tabular-nums">{format(peak)}</span>
      </div>
      <AreaSparkline points={vals} emptyLabel={emptyLabel} height={44} format={format} />
    </div>
  )
}

export function PerformancePage() {
  const { t } = useAppTranslation()
  const q = useVolumes()
  const metrics = useClusterMetrics()
  const attached = (q.data ?? []).filter((v) => (v.state ?? '').toLowerCase() === 'attached')
  const series = metrics.data?.series ?? []

  const byVolume = new Map<string, typeof series>()
  for (const s of series) {
    const vol = s.labels?.volume
    if (!vol) continue
    if (!byVolume.has(vol)) byVolume.set(vol, [])
    byVolume.get(vol)!.push(s)
  }

  return (
    <div data-testid="performance-page">
      <PageHeader
        title={t('performance.liveIoTitle')}
        description={t('performance.liveIoDescription')}
      />
      {metrics.data?.scrapeError ? (
        <p className="mb-3 text-sm text-amber-700 dark:text-amber-300">
          {t('performance.scrape', { error: metrics.data.scrapeError })}
        </p>
      ) : null}
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        onRetry={() => void q.refetch()}
      >
        <div className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {[...byVolume.entries()].map(([vol, ss]) => {
            const read = ss.find((s) => s.name.includes('read_throughput'))
            const write = ss.find((s) => s.name.includes('write_throughput'))
            const readIops = ss.find((s) => s.name.includes('read_iops'))
            const writeIops = ss.find((s) => s.name.includes('write_iops'))
            const mbps = (v: number) => `${formatBytes(v)}/s`
            const iops = (v: number) => `${Math.round(v).toLocaleString()} IOPS`
            return (
              <Card key={vol}>
                <CardHeader className="flex-row items-center justify-between space-y-0">
                  <CardTitle className="truncate">
                    <Link
                      to={`/volumes/${encodeURIComponent(vol)}`}
                      className="hover:underline"
                    >
                      {vol}
                    </Link>
                  </CardTitle>
                  <Activity size={16} className="text-[var(--color-primary)]" />
                </CardHeader>
                <CardContent className="grid grid-cols-2 gap-x-4 gap-y-3">
                  <MetricLine
                    label={t('performance.readThroughput')}
                    points={read?.points ?? []}
                    format={mbps}
                    emptyLabel={t('performance.noSamples')}
                  />
                  <MetricLine
                    label={t('performance.writeThroughput')}
                    points={write?.points ?? []}
                    format={mbps}
                    emptyLabel={t('performance.noSamples')}
                  />
                  <MetricLine
                    label={t('performance.readIops')}
                    points={readIops?.points ?? []}
                    format={iops}
                    emptyLabel={t('performance.noSamples')}
                  />
                  <MetricLine
                    label={t('performance.writeIops')}
                    points={writeIops?.points ?? []}
                    format={iops}
                    emptyLabel={t('performance.noSamples')}
                  />
                </CardContent>
              </Card>
            )
          })}
          {byVolume.size === 0 &&
            (attached.length === 0 ? (
              <Card>
                <CardContent className="flex items-center gap-2 py-8 text-sm text-[var(--color-muted-foreground)]">
                  <Activity size={18} /> {t('performance.noMetrics')}
                </CardContent>
              </Card>
            ) : (
              attached.map((v) => (
                <Card key={v.id ?? v.name}>
                  <CardHeader className="flex-row items-center justify-between space-y-0">
                    <CardTitle>{v.name}</CardTitle>
                    <Badge tone={stateTone(v.robustness)}>{v.robustness ?? v.state}</Badge>
                  </CardHeader>
                  <CardContent className="text-sm text-[var(--color-muted-foreground)]">
                    {t('performance.waitingScrape')}
                  </CardContent>
                </Card>
              ))
            ))}
        </div>
      </QueryState>
    </div>
  )
}

export function BenchmarksPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useBenchmarks()
  const create = useCreateBenchmark()
  const del = useDeleteBenchmark()
  const nodesQ = useNodes()
  const [profile, setProfile] = useState('quick')
  const [nodeName, setNodeName] = useState('')

  const nodeNames = (nodesQ.data ?? []).map((n) => n.name).filter(Boolean)

  return (
    <div data-testid="benchmarks-page">
      <PageHeader
        title={t('performance.benchmarksTitle')}
        description={t('performance.benchmarksDescription')}
        actions={
          canMutate ? (
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={profile}
                onChange={(e) => setProfile(e.target.value)}
                className="h-8 w-auto"
                aria-label={t('performance.profile')}
                disabled={create.isPending}
              >
                <option value="quick">{t('performance.profileQuick')}</option>
                <option value="standard">{t('performance.profileStandard')}</option>
                <option value="thorough">{t('performance.profileThorough')}</option>
              </Select>
              <Select
                value={nodeName}
                onChange={(e) => setNodeName(e.target.value)}
                className="h-8 w-auto"
                aria-label={t('common.node')}
                disabled={create.isPending}
              >
                <option value="">{t('performance.anyNode')}</option>
                {nodeNames.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </Select>
              <Button
                type="button"
                size="sm"
                data-testid="run-benchmark"
                disabled={create.isPending}
                onClick={() =>
                  void create.mutateAsync({
                    profile,
                    type: 'Disk',
                    // Omit nodeName for "any node" so the scheduler places it;
                    // otherwise target the chosen node.
                    ...(nodeName ? { nodeName } : {}),
                  })
                }
              >
                <Gauge size={14} /> {t('performance.runBenchmark')}
              </Button>
            </div>
          ) : null
        }
      />
      {q.error ? (
        <p className="text-sm text-red-600">{(q.error as Error).message}</p>
      ) : null}
      <div className="space-y-2">
        {(q.data?.data ?? []).map((b) => (
          <Card key={String(b.name)}>
            <CardContent className="flex flex-wrap items-center justify-between gap-2 py-4 text-sm">
              <div>
                <div className="font-medium">{String(b.name)}</div>
                <div className="text-xs text-[var(--color-muted-foreground)]">
                  {String(b.profile)} · {String(b.phase)} · {String(b.message ?? '')}
                </div>
                {b.results && typeof b.results === 'object' ? (
                  <div className="mt-1 font-mono text-xs">
                    {Object.entries(b.results as Record<string, number>)
                      .map(([k, v]) => `${k}=${typeof v === 'number' ? v.toFixed(1) : v}`)
                      .join(' · ')}
                  </div>
                ) : null}
                {b.fioCmd ? (
                  <pre className="mt-1 max-w-xl overflow-auto text-[10px] text-[var(--color-muted-foreground)]">
                    {String(b.fioCmd)}
                  </pre>
                ) : null}
              </div>
              {canMutate ? (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => void del.mutateAsync(String(b.name))}
                >
                  {t('common.delete')}
                </Button>
              ) : null}
            </CardContent>
          </Card>
        ))}
        {!q.isLoading && !(q.data?.data ?? []).length ? (
          <p className="text-sm text-[var(--color-muted-foreground)]">{t('performance.empty')}</p>
        ) : null}
      </div>
    </div>
  )
}
