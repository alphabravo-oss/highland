import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, CheckCircle2, ChevronRight, Database, Eye, EyeOff, Gauge, Layers3, LoaderCircle, Server, Trash2 } from 'lucide-react'
import {
  type Benchmark,
  useBenchmark,
  useBenchmarks,
  useCreateBenchmark,
  useDeleteBenchmark,
  useNodes,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader } from '@/components/data/PageHeader'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { QueryState } from '@/components/data/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { useStorageList } from '@/api/storage/hooks'
import type { StorageClassSummary } from '@/api/storage/types'

// Human-readable rendering for the raw fio result keys.
const BENCH_METRICS: Array<{ key: string; labelKey: string; fmt: (v: number) => string }> = [
  { key: 'seqReadMBps', labelKey: 'performance.benchSeqRead', fmt: (v) => `${v.toFixed(0)} MB/s` },
  { key: 'seqWriteMBps', labelKey: 'performance.benchSeqWrite', fmt: (v) => `${v.toFixed(0)} MB/s` },
  { key: 'randReadIOPS', labelKey: 'performance.benchRandRead', fmt: (v) => `${Math.round(v).toLocaleString()} IOPS` },
  { key: 'randWriteIOPS', labelKey: 'performance.benchRandWrite', fmt: (v) => `${Math.round(v).toLocaleString()} IOPS` },
  { key: 'latReadUs', labelKey: 'performance.benchReadLat', fmt: (v) => `${(v / 1000).toFixed(2)} ms` },
  { key: 'latWriteUs', labelKey: 'performance.benchWriteLat', fmt: (v) => `${(v / 1000).toFixed(2)} ms` },
]

function benchmarkProviderLabel(providerId: unknown) {
  const id = String(providerId ?? '')
  if (id === 'longhorn') return 'Longhorn'
  if (id === 'rook-ceph') return 'Rook / Ceph'
  if (id === 'openebs') return 'OpenEBS'
  if (id === 'linstor') return 'Piraeus / LINSTOR'
  return id || 'Unknown provider'
}

function benchmarkTime(value: unknown) {
  if (!value) return '—'
  const date = new Date(String(value))
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString()
}

type FormattedMetric = { key: string; label: string; value: string }

function BenchmarkTableRows({
  benchmark,
  expanded,
  canMutate,
  onToggle,
  onDelete,
  formatResults,
}: {
  benchmark: Benchmark
  expanded: boolean
  canMutate: boolean
  onToggle: () => void
  onDelete: () => void
  formatResults: (results: Record<string, number>) => FormattedMetric[]
}) {
  const { t } = useAppTranslation()
  const detailQuery = useBenchmark(expanded ? benchmark.name : null)
  const detail = detailQuery.data ?? benchmark
  const metrics = detail.results ? formatResults(detail.results) : []
  const active = detail.phase === 'Pending' || detail.phase === 'Running'

  return <>
    <TR data-testid={`benchmark-row-${benchmark.name}`}>
      <TD>
        <button type="button" className="group flex max-w-64 items-center gap-2 text-left" onClick={onToggle} aria-expanded={expanded} aria-controls={`benchmark-details-${benchmark.name}`}>
          <ChevronRight size={14} className={`shrink-0 text-[var(--color-muted-foreground)] transition-transform ${expanded ? 'rotate-90' : ''}`} aria-hidden />
          <span className="min-w-0"><span className="block truncate font-medium group-hover:text-[var(--color-primary)]">{benchmark.name}</span><span className="block whitespace-nowrap text-[10px] text-[var(--color-muted-foreground)]">{benchmarkTime(benchmark.createdAt)}</span></span>
        </button>
      </TD>
      <TD><Badge tone="primary">{benchmarkProviderLabel(benchmark.providerId)}</Badge></TD>
      <TD className="max-w-48"><span className="block truncate" title={benchmark.storageClass}>{benchmark.storageClass || '—'}</span></TD>
      <TD className="capitalize">{benchmark.profile}</TD>
      <TD><div className="flex flex-col items-start gap-1"><Badge tone={stateTone(benchmark.phase)}>{active ? <LoaderCircle size={12} className="mr-1 inline animate-spin" /> : null}{benchmark.phase}</Badge><span className="text-[10px] text-[var(--color-muted-foreground)]">{benchmark.mode ?? 'unknown'}</span></div></TD>
      <TD>{benchmark.nodeName || t('performance.schedulerSelected')}</TD>
      <TD><div className="flex justify-end gap-1">
        <Button type="button" size="icon" variant="ghost" onClick={onToggle} aria-expanded={expanded} aria-label={expanded ? t('performance.hideRunDetails', { name: benchmark.name }) : t('performance.viewRunDetails', { name: benchmark.name })}>{expanded ? <EyeOff size={15} /> : <Eye size={15} />}</Button>
        {canMutate ? <Button type="button" size="icon" variant="ghost" onClick={onDelete} aria-label={t('performance.deleteRun', { name: benchmark.name })}><Trash2 size={15} /></Button> : null}
      </div></TD>
    </TR>
    {expanded ? <TR id={`benchmark-details-${benchmark.name}`} className="bg-[var(--color-muted)]/20 hover:bg-[var(--color-muted)]/20">
      <TD colSpan={7} className="p-0">
        <div className="border-l-2 border-[var(--color-primary)] px-4 py-4" data-testid={`benchmark-details-${benchmark.name}`}>
          {detailQuery.isLoading ? <div role="status" className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]"><LoaderCircle size={15} className="animate-spin" />{t('performance.loadingRunDetails')}</div> : detailQuery.error ? <Alert tone="danger">{detailQuery.error instanceof Error ? detailQuery.error.message : t('performance.detailsUnavailable')}</Alert> : <div className="space-y-4">
            <div>
              <h3 className="text-sm font-semibold">{t('performance.results')}</h3>
              {metrics.length ? <div className="mt-2 grid gap-3 sm:grid-cols-3 xl:grid-cols-6">{metrics.map((metric) => <div key={metric.key} className="rounded-md border border-[var(--color-border)] bg-[var(--color-background)] p-3"><div className="text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{metric.label}</div><div className="mt-1 tabular-nums font-semibold">{metric.value}</div></div>)}</div> : <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{active ? t('performance.resultsPending') : t('performance.noResultsRecorded')}</p>}
            </div>
            <div className="grid gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-background)] p-3 sm:grid-cols-2 xl:grid-cols-4" data-testid={`benchmark-target-${benchmark.name}`}>
              <BenchmarkFact icon={Database} label={t('performance.storageProvider')} value={benchmarkProviderLabel(detail.providerId)} />
              <BenchmarkFact icon={Layers3} label={t('performance.storageClass')} value={detail.storageClass || '—'} />
              <BenchmarkFact icon={Gauge} label={t('performance.provisionerDriver')} value={detail.csiDriver || '—'} mono />
              <BenchmarkFact icon={Server} label={t('common.node')} value={detail.nodeName || t('performance.schedulerSelected')} />
              <BenchmarkFact label="PVC / PV" value={[detail.pvcName, detail.pvName].filter(Boolean).join(' / ') || '—'} />
              <BenchmarkFact label={t('performance.started')} value={benchmarkTime(detail.createdAt)} />
              <BenchmarkFact label={t('performance.completed')} value={benchmarkTime(detail.completedAt)} />
              <BenchmarkFact label={t('performance.message')} value={detail.message || '—'} />
            </div>
            {detail.fioCmd ? <div><h3 className="text-sm font-semibold">{t('performance.fioCommand')}</h3><pre className="mt-2 overflow-auto rounded-md bg-[var(--color-muted)] p-3 text-[11px] text-[var(--color-muted-foreground)]">{detail.fioCmd}</pre></div> : null}
          </div>}
        </div>
      </TD>
    </TR> : null}
  </>
}

function BenchmarkFact({ icon: Icon, label, value, mono = false }: { icon?: typeof Database; label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0"><div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{Icon ? <Icon size={13} aria-hidden /> : null}{label}</div><div className={`mt-1 break-words text-sm font-medium ${mono ? 'font-mono text-xs text-[var(--color-primary)]' : ''}`}>{value}</div></div>
}

export function BenchmarksPage() {
  const { t } = useAppTranslation()
  const { canMutate, isAdmin } = useAuth()
  const [cursor, setCursor] = useState('')
  const q = useBenchmarks(cursor)
  const create = useCreateBenchmark()
  const del = useDeleteBenchmark()
  const nodesQ = useNodes()
  const classesQ = useStorageList<StorageClassSummary>('classes', { limit: 500 })
  const [profile, setProfile] = useState('quick')
  const [nodeName, setNodeName] = useState('')
  const [storageClass, setStorageClass] = useState('')
  const [retainFailedPvc, setRetainFailedPvc] = useState(false)
  const [retainConfirmation, setRetainConfirmation] = useState('')
  const [deleteBench, setDeleteBench] = useState<string | null>(null)
  const [expandedBench, setExpandedBench] = useState<string | null>(null)

  const nodeNames = (nodesQ.data ?? []).map((n) => n.name).filter(Boolean)
  const storageClasses = useMemo(() => classesQ.data?.data ?? [], [classesQ.data?.data])

  useEffect(() => {
    if (!storageClass && storageClasses.length > 0) {
      setStorageClass(storageClasses.find((item) => item.default)?.name ?? storageClasses[0]?.name ?? '')
    }
  }, [storageClass, storageClasses])

  const selectedClass = storageClasses.find((item) => item.name === storageClass)
  const benchmarkMode = q.data?.meta.benchmarkMode
  const realBenchmarksEnabled = benchmarkMode === 'kubernetes-job'

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
          <Badge
            tone={q.isLoading ? 'default' : realBenchmarksEnabled ? 'success' : 'warning'}
            title={realBenchmarksEnabled ? t('performance.realModeDescription') : undefined}
            data-testid={realBenchmarksEnabled ? 'benchmark-mode-enabled' : 'benchmark-mode-status'}
          >
            {q.isLoading ? <LoaderCircle size={12} className="mr-1 inline animate-spin" /> : realBenchmarksEnabled ? <CheckCircle2 size={12} className="mr-1 inline" /> : <AlertTriangle size={12} className="mr-1 inline" />}
            {q.isLoading ? t('performance.modeChecking') : realBenchmarksEnabled ? t('performance.realModeCompact') : t('performance.disabledModeCompact')}
          </Badge>
        }
      />
      {canMutate ? (
        <Card className="mb-4" data-testid="benchmark-run-panel">
          <CardHeader>
            <CardTitle>{t('performance.runPanelTitle')}</CardTitle>
            <p className="text-sm text-[var(--color-muted-foreground)]">{t('performance.runPanelDescription')}</p>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-[minmax(13rem,.8fr)_minmax(18rem,1.35fr)_minmax(11rem,.7fr)]">
              <div className="space-y-1.5">
                <Label htmlFor="benchmark-profile">{t('performance.profile')}</Label>
                <Select
                  id="benchmark-profile"
                  value={profile}
                  onChange={(e) => setProfile(e.target.value)}
                  aria-label={t('performance.profile')}
                  disabled={create.isPending}
                >
                  <option value="quick">{t('performance.profileQuick')}</option>
                  <option value="standard">{t('performance.profileStandard')}</option>
                  <option value="thorough">{t('performance.profileThorough')}</option>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="benchmark-storage-class">{t('performance.storageClass')}</Label>
                <Select
                  id="benchmark-storage-class"
                  value={storageClass}
                  onChange={(e) => setStorageClass(e.target.value)}
                  aria-label={t('performance.storageClass')}
                  disabled={create.isPending || classesQ.isLoading}
                >
                  <option value="">{t('performance.selectStorageClass')}</option>
                  {storageClasses.map((item) => (
                    <option key={item.name} value={item.name}>
                      {item.name} · {item.provisioner}
                    </option>
                  ))}
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="benchmark-node">{t('common.node')}</Label>
                <Select
                  id="benchmark-node"
                  value={nodeName}
                  onChange={(e) => setNodeName(e.target.value)}
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
              </div>
            </div>

            <div className="mt-4 flex flex-col gap-3 border-t border-[var(--color-border)] pt-4 lg:flex-row lg:items-end lg:justify-between">
              <div className="space-y-2">
                {isAdmin ? (
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={retainFailedPvc}
                      onChange={(event) => {
                        setRetainFailedPvc(event.target.checked)
                        if (!event.target.checked) setRetainConfirmation('')
                      }}
                    />
                    <span>{t('performance.retainFailedPvc')}</span>
                  </label>
                ) : null}
                {retainFailedPvc ? (
                  <input
                    className="h-9 w-full max-w-xs rounded-md border bg-transparent px-3 text-sm"
                    aria-label="Retain failed PVC confirmation"
                    placeholder="RETAIN FAILED PVC"
                    value={retainConfirmation}
                    onChange={(event) => setRetainConfirmation(event.target.value)}
                  />
                ) : null}
                {selectedClass ? (
                  <p className="text-xs text-[var(--color-muted-foreground)]">
                    {t('performance.selectedProvider', {
                      provider: selectedClass.providerId,
                      provisioner: selectedClass.provisioner,
                    })}
                  </p>
                ) : null}
              </div>

              <Button
                type="button"
                data-testid="run-benchmark"
                className="w-full lg:w-auto"
                disabled={!realBenchmarksEnabled || create.isPending || !storageClass || (retainFailedPvc && retainConfirmation !== 'RETAIN FAILED PVC')}
                onClick={() =>
                  void create.mutateAsync({
                    profile,
                    type: 'Disk',
                    storageClass,
                    accessMode: 'ReadWriteOnce',
                    volumeMode: 'Filesystem',
                    retainFailedPvc,
                    ...(retainFailedPvc ? { retainConfirmation } : {}),
                    ...(nodeName ? { nodeName } : {}),
                  })
                }
              >
                <Gauge size={15} /> {create.isPending ? t('performance.startingBenchmark') : t('performance.runBenchmark')}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}
      {!q.isLoading && !realBenchmarksEnabled ? (
        <Alert tone="warning" className="mb-4" data-testid="benchmark-mode-disabled">
          <AlertTitle className="flex items-center gap-2"><AlertTriangle size={16} />{t('performance.disabledMode')}</AlertTitle>
          <AlertDescription>{t('performance.disabledModeDescription', { mode: benchmarkMode ?? 'unknown' })}</AlertDescription>
        </Alert>
      ) : null}
      {create.error ? (
        <Alert tone="danger" className="mb-4">
          <AlertTitle>{t('performance.startFailed')}</AlertTitle>
          <AlertDescription>{create.error instanceof Error ? create.error.message : String(create.error)}</AlertDescription>
        </Alert>
      ) : null}
      <QueryState
        isLoading={q.isLoading}
        isFetching={q.isFetching && !q.isLoading}
        observedAt={q.data?.meta.observedAt}
        stale={q.data?.meta.stale}
        partial={q.data?.meta.partial}
        error={q.error as Error | null}
        isEmpty={!(q.data?.data ?? []).length}
        emptyTitle={t('performance.empty')}
        onRetry={() => void q.refetch()}
      >
        <div className="space-y-2">
          <Table aria-label={t('performance.benchmarkRuns')}>
            <THead><TR>
              <TH>{t('performance.run')}</TH>
              <TH>{t('performance.storageProvider')}</TH>
              <TH>{t('performance.storageClass')}</TH>
              <TH>{t('performance.profile')}</TH>
              <TH>{t('performance.status')}</TH>
              <TH>{t('common.node')}</TH>
              <TH className="w-20 text-right">{t('common.actions')}</TH>
            </TR></THead>
            <TBody>
              {(q.data?.data ?? []).map((b) => {
                const name = String(b.name)
                const expanded = expandedBench === name
                return (
                  <BenchmarkTableRows
                    key={name}
                    benchmark={b as Benchmark}
                    expanded={expanded}
                    canMutate={canMutate}
                    onToggle={() => setExpandedBench(expanded ? null : name)}
                    onDelete={() => setDeleteBench(name)}
                    formatResults={formatBenchResults}
                  />
                )
              })}
            </TBody>
          </Table>
          {q.data?.page ? (
            <div className="flex items-center justify-between gap-3 pt-2 text-sm text-[var(--color-muted-foreground)]">
              <span>{q.data.page.total} benchmark runs</span>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" disabled={!cursor} onClick={() => setCursor('')}>
                  First page
                </Button>
                <Button type="button" size="sm" variant="outline" disabled={!q.data.page.continue} onClick={() => setCursor(q.data?.page.continue ?? '')}>
                  Next page
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </QueryState>

      <ConfirmDialog
        open={Boolean(deleteBench)}
        onOpenChange={(v) => !v && setDeleteBench(null)}
        title={t('performance.deleteBenchmark')}
        description={deleteBench ? t('performance.deleteBenchmarkConfirm', { name: deleteBench }) : ''}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={async () => {
          if (deleteBench) await del.mutateAsync(deleteBench)
          setDeleteBench(null)
        }}
      />
    </div>
  )
}
