import { Activity, Gauge } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  useBenchmarks,
  useClusterMetrics,
  useCreateBenchmark,
  useDeleteBenchmark,
  useVolumes,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppTranslation } from '@/i18n/useAppTranslation'

function Sparkline({ points, emptyLabel }: { points: Array<{ v: number }>; emptyLabel: string }) {
  if (!points.length) {
    return (
      <div className="h-10 text-xs text-[var(--color-muted-foreground)]">
        {emptyLabel}
      </div>
    )
  }
  const vals = points.map((p) => p.v)
  const min = Math.min(...vals)
  const max = Math.max(...vals)
  const w = 160
  const h = 40
  const span = max - min || 1
  const d = vals
    .map((v, i) => {
      const x = (i / Math.max(vals.length - 1, 1)) * w
      const y = h - ((v - min) / span) * (h - 4) - 2
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  return (
    <svg width={w} height={h} className="overflow-visible" aria-hidden>
      <path d={d} fill="none" stroke="var(--color-primary)" strokeWidth="1.75" />
    </svg>
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
            return (
              <Card key={vol}>
                <CardHeader className="flex-row items-center justify-between space-y-0">
                  <CardTitle>
                    <Link
                      to={`/volumes/${encodeURIComponent(vol)}`}
                      className="hover:underline"
                    >
                      {vol}
                    </Link>
                  </CardTitle>
                  <Activity size={16} className="text-[var(--color-primary)]" />
                </CardHeader>
                <CardContent className="space-y-2 text-xs text-[var(--color-muted-foreground)]">
                  <div>
                    <div className="mb-1 font-medium text-[var(--color-foreground)]">
                      {t('performance.readThroughput')}
                    </div>
                    <Sparkline points={read?.points ?? []} emptyLabel={t('performance.noSamples')} />
                  </div>
                  <div>
                    <div className="mb-1 font-medium text-[var(--color-foreground)]">
                      {t('performance.writeThroughput')}
                    </div>
                    <Sparkline points={write?.points ?? []} emptyLabel={t('performance.noSamples')} />
                  </div>
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

  return (
    <div data-testid="benchmarks-page">
      <PageHeader
        title={t('performance.benchmarksTitle')}
        description={t('performance.benchmarksDescription')}
        actions={
          canMutate ? (
            <Button
              type="button"
              size="sm"
              data-testid="run-benchmark"
              disabled={create.isPending}
              onClick={() =>
                void create.mutateAsync({
                  profile: 'quick',
                  type: 'Disk',
                  nodeName: 'node-1',
                })
              }
            >
              <Gauge size={14} /> {t('performance.runQuick')}
            </Button>
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
