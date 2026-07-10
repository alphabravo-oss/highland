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
import { MetricLine } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'

// Human-readable rendering for the raw fio result keys.
const BENCH_METRICS: Array<{ key: string; labelKey: string; fmt: (v: number) => string }> = [
  { key: 'seqReadMBps', labelKey: 'performance.benchSeqRead', fmt: (v) => `${v.toFixed(0)} MB/s` },
  { key: 'seqWriteMBps', labelKey: 'performance.benchSeqWrite', fmt: (v) => `${v.toFixed(0)} MB/s` },
  { key: 'randReadIOPS', labelKey: 'performance.benchRandRead', fmt: (v) => `${Math.round(v).toLocaleString()} IOPS` },
  { key: 'randWriteIOPS', labelKey: 'performance.benchRandWrite', fmt: (v) => `${Math.round(v).toLocaleString()} IOPS` },
  { key: 'latReadUs', labelKey: 'performance.benchReadLat', fmt: (v) => `${(v / 1000).toFixed(2)} ms` },
  { key: 'latWriteUs', labelKey: 'performance.benchWriteLat', fmt: (v) => `${(v / 1000).toFixed(2)} ms` },
]

export function PerformancePage() {
  const { t } = useAppTranslation()
  const q = useVolumes()
  const metrics = useClusterMetrics()
  const attached = (q.data ?? []).filter((v) => (v.state ?? '').toLowerCase() === 'attached')
  const series = metrics.data?.series ?? []

  // Only show volumes that currently exist. The scraper retains a short ring of
  // samples, so recently-deleted volumes (e.g. finished benchmark PVCs) can
  // still have series until they age out — filter them against the live list.
  const liveVolumes = new Set((q.data ?? []).map((v) => v.name))

  const byVolume = new Map<string, typeof series>()
  for (const s of series) {
    const vol = s.labels?.volume
    if (!vol || !liveVolumes.has(vol)) continue
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
            const pv = (name: string) => (ss.find((s) => s.name.includes(name))?.points ?? []).map((p) => p.v)
            const read = pv('read_throughput')
            const write = pv('write_throughput')
            const readIops = pv('read_iops')
            const writeIops = pv('write_iops')
            const mbps = (v: number) => `${formatBytes(v)}/s`
            const iops = (v: number) => `${Math.round(v).toLocaleString()} IOPS`
            const peak = t('performance.peak')
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
                  <MetricLine label={t('performance.readThroughput')} points={read} format={mbps} emptyLabel={t('performance.noSamples')} peakLabel={peak} />
                  <MetricLine label={t('performance.writeThroughput')} points={write} format={mbps} emptyLabel={t('performance.noSamples')} peakLabel={peak} />
                  <MetricLine label={t('performance.readIops')} points={readIops} format={iops} emptyLabel={t('performance.noSamples')} peakLabel={peak} />
                  <MetricLine label={t('performance.writeIops')} points={writeIops} format={iops} emptyLabel={t('performance.noSamples')} peakLabel={peak} />
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

  function formatBenchResults(results: Record<string, number>) {
    const out: Array<{ key: string; label: string; value: string }> = []
    for (const m of BENCH_METRICS) {
      const val = results[m.key]
      if (typeof val === 'number') {
        out.push({ key: m.key, label: t(m.labelKey), value: m.fmt(val) })
      }
    }
    for (const [k, v] of Object.entries(results)) {
      if (!BENCH_METRICS.some((m) => m.key === k)) {
        out.push({ key: k, label: k, value: typeof v === 'number' ? v.toFixed(1) : String(v) })
      }
    }
    return out
  }

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
                  <div className="mt-2 grid max-w-lg grid-cols-2 gap-x-6 gap-y-1.5 sm:grid-cols-3">
                    {formatBenchResults(b.results as Record<string, number>).map((m) => (
                      <div key={m.key}>
                        <div className="text-[10px] uppercase tracking-wide text-[var(--color-muted-foreground)]">
                          {m.label}
                        </div>
                        <div className="tabular-nums font-semibold">{m.value}</div>
                      </div>
                    ))}
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
