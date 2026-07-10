import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Camera, RefreshCw } from 'lucide-react'
import {
  useEngineImages,
  useNodes,
  useRecurringJobs,
  useVolume,
  useVolumeAction,
  useVolumeMetrics,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import {
  eventsApi,
  execAction,
  formatBytes,
  hasAction,
  toConditionArray,
  type LHResource,
  type Volume,
  volumeAttachmentsApi,
} from '@/api/longhorn'
import { MetricLine } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { ActionFormDialog } from './ActionFormDialog'
import { VOLUME_ACTION_DEFS, volumeActionLabel, type VolumeActionDef } from './volumeActions'

type SnapshotRow = { name: string; created?: string; size?: string | number; [k: string]: unknown }


function InfoRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="grid grid-cols-3 gap-2 border-b border-[var(--color-border)] py-2 text-sm last:border-0">
      <div className="text-[var(--color-muted-foreground)]">{label}</div>
      <div className="col-span-2 break-all font-medium">{value ?? '—'}</div>
    </div>
  )
}

function normalizeSnapshots(result: unknown): SnapshotRow[] {
  if (!result) return []
  if (Array.isArray(result)) {
    return result.map((s) => {
      if (typeof s === 'string') return { name: s }
      const o = s as Record<string, unknown>
      return { name: String(o.name ?? o.id ?? 'unknown'), created: o.created as string | undefined, size: o.size as string | number | undefined, ...o }
    })
  }
  if (typeof result === 'object' && result !== null) {
    const o = result as Record<string, unknown>
    if (Array.isArray(o.data)) return normalizeSnapshots(o.data)
    return Object.entries(o)
      .filter(([k]) => !['type', 'links', 'actions'].includes(k))
      .map(([name, v]) => {
        if (v && typeof v === 'object') {
          const s = v as Record<string, unknown>
          return { name: String(s.name ?? name), created: s.created as string | undefined, size: s.size as string | number | undefined, ...s }
        }
        return { name }
      })
  }
  return []
}

export function VolumeDetailPage() {
  const { t } = useAppTranslation()
  const { name = '' } = useParams()
  const volName = decodeURIComponent(name)
  const { canMutate } = useAuth()
  const q = useVolume(volName)
  const metrics = useVolumeMetrics(volName)
  const nodesQ = useNodes()
  const imagesQ = useEngineImages()
  const jobsQ = useRecurringJobs()
  const actionMut = useVolumeAction()

  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [snapshots, setSnapshots] = useState<SnapshotRow[]>([])
  const [snapLoading, setSnapLoading] = useState(false)
  const [snapCreateOpen, setSnapCreateOpen] = useState(false)
  const [snapName, setSnapName] = useState('')
  const [actionDef, setActionDef] = useState<VolumeActionDef | null>(null)
  const [jobAttachOpen, setJobAttachOpen] = useState(false)
  const [jobName, setJobName] = useState('')
  const [events, setEvents] = useState<LHResource[]>([])
  const [attachments, setAttachments] = useState<LHResource[]>([])

  const vol = q.data
  const hosts = (nodesQ.data ?? []).map((n) => n.name)
  const images = (imagesQ.data ?? []).map((i) => i.image ?? i.name).filter(Boolean) as string[]

  const refreshSnapshots = useCallback(async (volume: Volume) => {
    if (!hasAction(volume, 'snapshotList')) {
      if (Array.isArray(volume.snapshots)) setSnapshots(normalizeSnapshots(volume.snapshots))
      return
    }
    setSnapLoading(true)
    try {
      setSnapshots(normalizeSnapshots(await execAction(volume, 'snapshotList', {})))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'snapshotList failed')
    } finally {
      setSnapLoading(false)
    }
  }, [])

  useEffect(() => {
    if (vol) void refreshSnapshots(vol)
  }, [vol, refreshSnapshots])

  useEffect(() => {
    let c = false
    void (async () => {
      try {
        const all = await eventsApi.list()
        if (!c) {
          setEvents(
            all
              .filter((e) => {
                const inv = e.involvedObject as { name?: string } | undefined
                return inv?.name === volName || String(e.message ?? '').includes(volName)
              })
              .slice(0, 30),
          )
        }
      } catch {
        if (!c) setEvents([])
      }
      try {
        const atts = await volumeAttachmentsApi.list()
        if (!c) {
          setAttachments(
            atts.filter((a) => a.id === volName || a.name === volName || String(a.id).includes(volName)),
          )
        }
      } catch {
        if (!c) setAttachments([])
      }
    })()
    return () => {
      c = true
    }
  }, [volName])

  async function runAction(volume: Volume, action: string, params: Record<string, unknown> = {}) {
    setError(null)
    setSuccess(null)
    try {
      await actionMut.mutateAsync({ vol: volume, action, params })
      setSuccess(t('volumeDetail.actionOk', { action }))
      const fresh = await q.refetch()
      if (fresh.data) await refreshSnapshots(fresh.data)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('volumeActions.actionFailed'))
    }
  }

  const availableActions = VOLUME_ACTION_DEFS.filter((d) => vol && hasAction(vol, d.key))

  const readSeries = metrics.data?.series?.find((s) => s.name.includes('read_throughput'))
  const writeSeries = metrics.data?.series?.find((s) => s.name.includes('write_throughput'))
  const readIops = metrics.data?.series?.find((s) => s.name.includes('read_iops'))
  const writeIops = metrics.data?.series?.find((s) => s.name.includes('write_iops'))
  const pts = (s?: { points?: Array<{ v: number }> }) => (s?.points ?? []).map((p) => p.v)

  return (
    <div data-testid="volume-detail-page">
      <div className="mb-3 flex flex-wrap gap-3 text-sm">
        <Link to="/volumes" className="inline-flex items-center gap-1 text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]">
          <ArrowLeft size={14} /> {t('volumeDetail.backToVolumes')}
        </Link>
        <Link to="/backups" className="text-[var(--color-primary)] hover:underline">
          {t('volumeDetail.backupsLink')}
        </Link>
      </div>
      <PageHeader
        title={volName}
        description={t('volumeDetail.description')}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
            <RefreshCw size={14} /> {t('common.refresh')}
          </Button>
        }
      />
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}
      {success ? <Alert tone="success" className="mb-3">{success}</Alert> : null}

      <QueryState isLoading={q.isLoading} error={q.error as Error | null} onRetry={() => void q.refetch()}>
        {vol ? (
          <div className="space-y-4">
            <div className="flex flex-wrap gap-2">
              <Badge tone={stateTone(vol.state)}>{vol.state ?? '—'}</Badge>
              <Badge tone={stateTone(vol.robustness)}>{vol.robustness ?? '—'}</Badge>
              {vol.dataEngine ? <Badge>{vol.dataEngine}</Badge> : null}
              {vol.standby ? <Badge tone="warning">{t('volumeDetail.standbyDr')}</Badge> : null}
            </div>

            <div className="flex flex-wrap gap-1" data-testid="volume-actions">
              {availableActions.map((d) => (
                <Button
                  key={d.key}
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={!canMutate}
                  onClick={() => {
                    if (d.key === 'snapshotCreate') {
                      setSnapCreateOpen(true)
                      return
                    }
                    if (d.key === 'recurringJobAdd') {
                      setJobAttachOpen(true)
                      return
                    }
                    setActionDef(d as VolumeActionDef)
                  }}
                >
                  {volumeActionLabel(t, d.key, d.label)}
                </Button>
              ))}
              {/* Any extra actions not in our def list */}
              {Object.keys(vol.actions ?? {})
                .filter((k) => !VOLUME_ACTION_DEFS.some((d) => d.key === k) && !k.startsWith('snapshot'))
                .map((k) => (
                  <Button
                    key={k}
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={!canMutate}
                    onClick={() => void runAction(vol, k, {})}
                  >
                    {volumeActionLabel(t, k, k)}
                  </Button>
                ))}
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
              <Card>
                <CardHeader>
                  <CardTitle>{t('volumeDetail.info')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <InfoRow label={t('common.size')} value={formatBytes(vol.size)} />
                  <InfoRow label={t('volumeDetail.actualSize')} value={formatBytes(vol.actualSize)} />
                  <InfoRow label={t('volumeDetail.replicas')} value={vol.numberOfReplicas} />
                  <InfoRow label={t('volumes.frontend')} value={vol.frontend} />
                  <InfoRow label={t('volumes.accessMode')} value={vol.accessMode} />
                  <InfoRow label={t('volumes.dataLocality')} value={vol.dataLocality} />
                  <InfoRow label={t('volumeDetail.shareEndpoint')} value={vol.shareEndpoint} />
                  <InfoRow label={t('volumeDetail.latestBackup')} value={String((vol as { lastBackup?: string }).lastBackup ?? '—')} />
                  <InfoRow label={t('volumeDetail.pv')} value={vol.kubernetesStatus?.pvName} />
                  <InfoRow
                    label={t('volumeDetail.pvc')}
                    value={
                      vol.kubernetesStatus?.pvcName
                        ? `${vol.kubernetesStatus.namespace}/${vol.kubernetesStatus.pvcName}`
                        : undefined
                    }
                  />
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>{t('volumeDetail.liveIo')}</CardTitle>
                </CardHeader>
                <CardContent className="grid grid-cols-2 gap-x-4 gap-y-3">
                  <MetricLine
                    label={t('volumeDetail.readThroughput')}
                    points={pts(readSeries)}
                    format={(v) => `${formatBytes(v)}/s`}
                    emptyLabel={t('volumeDetail.noIo')}
                    peakLabel={t('common.peak')}
                    axis
                  />
                  <MetricLine
                    label={t('volumeDetail.writeThroughput')}
                    points={pts(writeSeries)}
                    format={(v) => `${formatBytes(v)}/s`}
                    emptyLabel={t('volumeDetail.noIo')}
                    peakLabel={t('common.peak')}
                    axis
                  />
                  <MetricLine
                    label={t('volumeDetail.readIops')}
                    points={pts(readIops)}
                    format={(v) => `${Math.round(v).toLocaleString()} IOPS`}
                    emptyLabel={t('volumeDetail.noIo')}
                    peakLabel={t('common.peak')}
                    axis
                  />
                  <MetricLine
                    label={t('volumeDetail.writeIops')}
                    points={pts(writeIops)}
                    format={(v) => `${Math.round(v).toLocaleString()} IOPS`}
                    emptyLabel={t('volumeDetail.noIo')}
                    peakLabel={t('common.peak')}
                    axis
                  />
                  {metrics.data?.scrapeError ? (
                    <p className="col-span-2 text-xs text-amber-600 dark:text-amber-300">
                      {t('volumeDetail.scrape', { error: metrics.data.scrapeError })}
                    </p>
                  ) : null}
                </CardContent>
              </Card>
            </div>

            <Card>
              <CardHeader>
                <CardTitle>{t('volumeDetail.conditions')}</CardTitle>
              </CardHeader>
              <CardContent>
                {toConditionArray(vol.conditions).map((c, i) => (
                  <div key={i} className="mb-2 rounded border border-[var(--color-border)] p-2 text-sm" title={c.message}>
                    <span className="font-medium">{c.type}</span>{' '}
                    <Badge tone={stateTone(c.status)}>{c.status}</Badge>
                    {c.message ? <p className="mt-1 text-[var(--color-muted-foreground)]">{c.message}</p> : null}
                  </div>
                ))}
                {!toConditionArray(vol.conditions).length ? (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noConditions')}</p>
                ) : null}
              </CardContent>
            </Card>

            <Card data-testid="snapshots-panel">
              <CardHeader className="flex-row items-center justify-between space-y-0">
                <CardTitle className="flex items-center gap-2">
                  <Camera size={16} /> {t('volumeDetail.snapshots')}
                </CardTitle>
                <div className="flex gap-2">
                  <Button type="button" size="sm" variant="outline" disabled={snapLoading} onClick={() => void refreshSnapshots(vol)}>
                    {t('common.refresh')}
                  </Button>
                  {hasAction(vol, 'snapshotCreate') && canMutate ? (
                    <Button type="button" size="sm" data-testid="snapshot-create" onClick={() => setSnapCreateOpen(true)}>
                      {t('volumeDetail.createSnapshot')}
                    </Button>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent>
                {snapshots.length === 0 ? (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noSnapshots')}</p>
                ) : (
                  <Table>
                    <THead>
                      <TR>
                        <TH>{t('common.name')}</TH>
                        <TH>{t('common.created')}</TH>
                        <TH>{t('common.size')}</TH>
                        <TH className="text-right">{t('common.actions')}</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {snapshots.map((s) => (
                        <TR key={s.name}>
                          <TD className="font-medium">{s.name}</TD>
                          <TD className="whitespace-nowrap text-xs">{s.created ?? '—'}</TD>
                          <TD className="tabular-nums">{formatBytes(s.size)}</TD>
                          <TD>
                            <div className="flex justify-end gap-1">
                              {hasAction(vol, 'snapshotRevert') ? (
                                <Button type="button" size="sm" variant="outline" disabled={!canMutate} onClick={() => void runAction(vol, 'snapshotRevert', { name: s.name })}>
                                  {t('volumeDetail.revert')}
                                </Button>
                              ) : null}
                              {hasAction(vol, 'snapshotBackup') ? (
                                <Button type="button" size="sm" variant="outline" disabled={!canMutate} onClick={() => void runAction(vol, 'snapshotBackup', { name: s.name, backupTargetName: '', labels: {} })}>
                                  {t('volumeDetail.backup')}
                                </Button>
                              ) : null}
                              {hasAction(vol, 'snapshotDelete') ? (
                                <Button type="button" size="sm" variant="ghost" disabled={!canMutate} onClick={() => void runAction(vol, 'snapshotDelete', { name: s.name })}>
                                  {t('common.delete')}
                                </Button>
                              ) : null}
                            </div>
                          </TD>
                        </TR>
                      ))}
                    </TBody>
                  </Table>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('volumeDetail.replicas')}</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <THead>
                    <TR>
                      <TH>{t('common.name')}</TH>
                      <TH>{t('common.node')}</TH>
                      <TH>{t('volumeDetail.disk')}</TH>
                      <TH>{t('volumeDetail.mode')}</TH>
                      <TH>{t('volumeDetail.running')}</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {(vol.replicas ?? []).map((r, i) => (
                      <TR key={r.name ?? i}>
                        <TD>{r.name ?? '—'}</TD>
                        <TD>
                          {r.hostId ? (
                            <Link to={`/nodes/${encodeURIComponent(r.hostId)}`} className="text-[var(--color-primary)] hover:underline">
                              {r.hostId}
                            </Link>
                          ) : (
                            '—'
                          )}
                        </TD>
                        <TD className="max-w-[12rem] truncate">{r.diskID ?? '—'}</TD>
                        <TD>{r.mode ?? '—'}</TD>
                        <TD>
                          <Badge tone={r.running ? 'success' : 'danger'}>{r.running ? t('common.yes') : t('common.no')}</Badge>
                        </TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('volumeDetail.workloads')}</CardTitle>
              </CardHeader>
              <CardContent>
                {(vol.kubernetesStatus?.workloadsStatus ?? []).length === 0 ? (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noWorkloads')}</p>
                ) : (
                  <Table>
                    <THead>
                      <TR>
                        <TH>{t('volumeDetail.pod')}</TH>
                        <TH>{t('common.status')}</TH>
                        <TH>{t('volumeDetail.workload')}</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {(vol.kubernetesStatus?.workloadsStatus ?? []).map((w, i) => (
                        <TR key={i}>
                          <TD>{w.podName}</TD>
                          <TD>
                            <Badge tone={stateTone(w.podStatus)}>{w.podStatus}</Badge>
                          </TD>
                          <TD>
                            {w.workloadType}/{w.workloadName}
                          </TD>
                        </TR>
                      ))}
                    </TBody>
                  </Table>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('volumeDetail.attachments')}</CardTitle>
              </CardHeader>
              <CardContent>
                {attachments.length === 0 ? (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noAttachments')}</p>
                ) : (
                  <pre className="max-h-40 overflow-auto text-xs">{JSON.stringify(attachments, null, 2)}</pre>
                )}
              </CardContent>
            </Card>

            <Card data-testid="volume-events">
              <CardHeader>
                <CardTitle>{t('volumeDetail.events')}</CardTitle>
              </CardHeader>
              <CardContent>
                {events.length === 0 ? (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noEvents')}</p>
                ) : (
                  <Table>
                    <THead>
                      <TR>
                        <TH>{t('common.reason')}</TH>
                        <TH>{t('common.message')}</TH>
                        <TH>{t('common.time')}</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {events.map((e) => (
                        <TR key={e.id}>
                          <TD>{String(e.reason ?? e.eventType ?? '—')}</TD>
                          <TD className="max-w-md truncate">{String(e.message ?? '—')}</TD>
                          <TD className="whitespace-nowrap text-xs">{String(e.lastTimestamp ?? '—')}</TD>
                        </TR>
                      ))}
                    </TBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </div>
        ) : null}
      </QueryState>

      <ActionFormDialog
        open={Boolean(actionDef)}
        onOpenChange={(v) => !v && setActionDef(null)}
        def={actionDef}
        hosts={hosts}
        images={images}
        replicas={(vol?.replicas ?? []).map((r) => r.name ?? '').filter(Boolean)}
        loading={actionMut.isPending}
        onSubmit={async (params) => {
          if (!vol || !actionDef) return
          await runAction(vol, actionDef.key, params)
        }}
      />

      <Dialog
        open={snapCreateOpen}
        onOpenChange={setSnapCreateOpen}
        title={t('volumeDetail.createSnapshot')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setSnapCreateOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              data-testid="snapshot-create-confirm"
              onClick={() => {
                if (!vol) return
                void runAction(vol, 'snapshotCreate', { name: snapName }).then(() => setSnapCreateOpen(false))
              }}
            >
              {t('common.create')}
            </Button>
          </>
        }
      >
        <Input value={snapName} onChange={(e) => setSnapName(e.target.value)} placeholder={t('volumeDetail.optionalName')} data-testid="snapshot-name" />
      </Dialog>

      <Dialog
        open={jobAttachOpen}
        onOpenChange={setJobAttachOpen}
        title={t('volumeDetail.attachRecurringJob')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setJobAttachOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              data-testid="recurring-job-attach"
              disabled={!jobName || !vol}
              onClick={() => {
                if (!vol) return
                void runAction(vol, 'recurringJobAdd', {
                  jobs: [{ name: jobName, isGroup: false }],
                }).then(() => setJobAttachOpen(false))
              }}
            >
              {t('common.attach')}
            </Button>
          </>
        }
      >
        <select
          className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
          value={jobName}
          onChange={(e) => setJobName(e.target.value)}
        >
          <option value="">{t('volumeDetail.selectJob')}</option>
          {(jobsQ.data ?? []).map((j) => (
            <option key={j.name} value={j.name}>
              {j.name} ({j.task})
            </option>
          ))}
        </select>
      </Dialog>
    </div>
  )
}
