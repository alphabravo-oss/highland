import { Activity } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useClusterMetrics, useVolumes } from '@/api/hooks'
import { formatBytes } from '@/api/longhorn'
import { MetricLine } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function LiveIOPage() {
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
