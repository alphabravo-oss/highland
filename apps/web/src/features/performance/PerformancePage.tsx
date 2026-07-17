import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Database, Gauge, Layers3, LoaderCircle, Server } from 'lucide-react'
import {
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
  return id || 'Unknown provider'
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

  const nodeNames = (nodesQ.data ?? []).map((n) => n.name).filter(Boolean)
  const storageClasses = classesQ.data?.data ?? []

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
      {q.isLoading ? (
        <Alert className="mb-4">
          <AlertTitle>{t('performance.modeChecking')}</AlertTitle>
          <AlertDescription>{t('performance.modeCheckingDescription')}</AlertDescription>
        </Alert>
      ) : realBenchmarksEnabled ? (
        <Alert tone="success" className="mb-4" data-testid="benchmark-mode-enabled">
          <AlertTitle className="flex items-center gap-2"><CheckCircle2 size={16} />{t('performance.realMode')}</AlertTitle>
          <AlertDescription>{t('performance.realModeDescription')}</AlertDescription>
        </Alert>
      ) : (
        <Alert tone="warning" className="mb-4" data-testid="benchmark-mode-disabled">
          <AlertTitle className="flex items-center gap-2"><AlertTriangle size={16} />{t('performance.disabledMode')}</AlertTitle>
          <AlertDescription>{t('performance.disabledModeDescription', { mode: benchmarkMode ?? 'unknown' })}</AlertDescription>
        </Alert>
      )}
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
          {(q.data?.data ?? []).map((b) => (
          <Card key={String(b.name)}>
            <CardContent className="flex flex-wrap items-center justify-between gap-2 py-4 text-sm">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="font-medium">{String(b.name)}</div>
                  <Badge tone={stateTone(String(b.phase))}>
                    {String(b.phase) === 'Pending' || String(b.phase) === 'Running' ? <LoaderCircle size={12} className="mr-1 inline animate-spin" /> : null}
                    {String(b.phase)}
                  </Badge>
                  <Badge tone="info">{String(b.mode ?? 'unknown')}</Badge>
                </div>
                <div className="text-xs text-[var(--color-muted-foreground)]">
                  {String(b.profile)} · {String(b.message ?? '')}
                </div>
                {b.storageClass ? (
                  <div className="mt-3 grid gap-3 rounded-lg border border-[var(--color-primary)]/25 bg-[var(--color-primary)]/5 p-3 sm:grid-cols-2 xl:grid-cols-4" data-testid={`benchmark-target-${String(b.name)}`}>
                    <div>
                      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]"><Database size={13} />{t('performance.storageProvider')}</div>
                      <Badge tone="primary" className="mt-1.5">{benchmarkProviderLabel(b.providerId)}</Badge>
                    </div>
                    <div>
                      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]"><Layers3 size={13} />{t('performance.storageClass')}</div>
                      <div className="mt-1.5 break-all font-semibold">{String(b.storageClass)}</div>
                    </div>
                    <div>
                      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]"><Gauge size={13} />{t('performance.provisionerDriver')}</div>
                      <div className="mt-1.5 break-all font-mono text-xs font-semibold text-[var(--color-primary)]">{String(b.csiDriver ?? 'Unknown')}</div>
                    </div>
                    <div>
                      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]"><Server size={13} />{t('common.node')}</div>
                      <div className="mt-1.5 font-semibold">{String(b.nodeName ?? t('performance.schedulerSelected'))}</div>
                    </div>
                    {(b.pvcName || b.pvName) ? (
                      <div className="text-xs text-[var(--color-muted-foreground)] sm:col-span-2 xl:col-span-4">
                        {b.pvcName ? `PVC ${String(b.pvcName)}` : ''}
                        {b.pvcName && b.pvName ? ' · ' : ''}
                        {b.pvName ? `PV ${String(b.pvName)}` : ''}
                      </div>
                    ) : null}
                  </div>
                ) : null}
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
                  aria-label={t('common.delete')}
                  onClick={() => setDeleteBench(String(b.name))}
                >
                  {t('common.delete')}
                </Button>
              ) : null}
            </CardContent>
          </Card>
          ))}
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
