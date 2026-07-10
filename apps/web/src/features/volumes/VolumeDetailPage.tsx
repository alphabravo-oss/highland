import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Camera, ChevronDown, RefreshCw, Trash2 } from 'lucide-react'
import {
  useBackupTargets,
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
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { MetricLine, UsageBar } from '@/components/data/dashcharts'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { SnapshotTree } from '@/components/data/SnapshotTree'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { ActionFormDialog } from './ActionFormDialog'
import { VOLUME_ACTION_DEFS, groupActions, volumeActionLabel, type VolumeActionDef } from './volumeActions'

type SnapshotRow = { name: string; created?: string; size?: string | number; [k: string]: unknown }

type AttachTicket = {
  id: string
  type?: string
  nodeID?: string
  satisfied?: boolean
  attachmentID?: string
}

// Flatten each VolumeAttachment CR's `attachments` ticket map into flat rows so
// the detail page can show a readable table instead of a raw JSON dump.
function attachmentTicketRows(atts: Array<Record<string, unknown>>): AttachTicket[] {
  const rows: AttachTicket[] = []
  for (const a of atts) {
    const map = (a.attachments as Record<string, Record<string, unknown>> | undefined) ?? {}
    for (const [id, tk] of Object.entries(map)) {
      rows.push({
        id,
        type: tk.attachmentType as string | undefined,
        nodeID: tk.nodeID as string | undefined,
        satisfied: tk.satisfied as boolean | undefined,
        attachmentID: tk.attachmentID as string | undefined,
      })
    }
  }
  return rows
}

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
  const backupTargetsQ = useBackupTargets()
  const actionMut = useVolumeAction()

  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [snapshots, setSnapshots] = useState<SnapshotRow[]>([])
  const [snapLoading, setSnapLoading] = useState(false)
  const [snapCreateOpen, setSnapCreateOpen] = useState(false)
  const [showSystemSnapshots, setShowSystemSnapshots] = useState(false)
  const [snapName, setSnapName] = useState('')
  const [actionDef, setActionDef] = useState<VolumeActionDef | null>(null)
  const [jobAttachOpen, setJobAttachOpen] = useState(false)
  const [jobName, setJobName] = useState('')
  const [events, setEvents] = useState<LHResource[]>([])
  const [attachments, setAttachments] = useState<LHResource[]>([])
  const [removeReplica, setRemoveReplica] = useState<string | null>(null)
  const [deleteSnapshot, setDeleteSnapshot] = useState<string | null>(null)
  const [backupSnap, setBackupSnap] = useState<string | null>(null)
  const [backupTargetName, setBackupTargetName] = useState('')
  const [backupMode, setBackupMode] = useState<'full' | 'incremental'>('incremental')

  const vol = q.data
  const backupTargetNames = (backupTargetsQ.data ?? [])
    .map((bt) => bt.name)
    .filter(Boolean) as string[]
  const hosts = (nodesQ.data ?? []).map((n) => n.name)
  const images = (imagesQ.data ?? []).map((i) => i.image ?? i.name).filter(Boolean) as string[]

  const refreshSnapshots = useCallback(async (volume: Volume) => {
    // Modern Longhorn exposes the CRD-backed snapshotCRList; older managers use
    // the legacy snapshotList. Prefer CR, fall back to legacy, then to inline.
    const listAction = hasAction(volume, 'snapshotCRList')
      ? 'snapshotCRList'
      : hasAction(volume, 'snapshotList')
        ? 'snapshotList'
        : null
    if (!listAction) {
      if (Array.isArray(volume.snapshots)) setSnapshots(normalizeSnapshots(volume.snapshots))
      return
    }
    setSnapLoading(true)
    try {
      setSnapshots(normalizeSnapshots(await execAction(volume, listAction, {})))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('volumeActions.actionFailed'))
    } finally {
      setSnapLoading(false)
    }
  }, [t])

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
  // Longhorn 1.12 exposes CR-backed snapshot actions (snapshotCRDelete/…) while
  // older managers use the legacy names. Prefer the CR variant when present so
  // per-snapshot delete/revert work across versions.
  const snapPref = (verb: string): string | null => {
    if (!vol) return null
    const cr = `snapshotCR${verb}`
    if (hasAction(vol, cr)) return cr
    const legacy = `snapshot${verb}`
    if (hasAction(vol, legacy)) return legacy
    return null
  }
  const extraActions = Object.keys(vol?.actions ?? {}).filter(
    (k) => !VOLUME_ACTION_DEFS.some((d) => d.key === k) && !k.startsWith('snapshot'),
  )

  // Restore progress: average per-replica progress while a restore/DR sync is in flight.
  const restoreStatus =
    (vol as { restoreStatus?: Array<{ replica?: string; isRestoring?: boolean; progress?: number; state?: string; error?: string }> } | undefined)
      ?.restoreStatus ?? []
  const restoreError = restoreStatus.find((r) => r.error)?.error
  const restoreActive =
    restoreStatus.some((r) => r.isRestoring) ||
    ((vol?.standby || (vol as { restoreRequired?: boolean } | undefined)?.restoreRequired) &&
      restoreStatus.some((r) => typeof r.progress === 'number'))
  const restorePercent = restoreStatus.length
    ? Math.floor(restoreStatus.reduce((sum, r) => sum + (r.progress ?? 0), 0) / restoreStatus.length)
    : 0
  const showRestoreProgress = Boolean(restoreActive) && restoreStatus.length > 0

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

            {canMutate && (availableActions.length > 0 || extraActions.length > 0) ? (
              <div className="flex flex-wrap gap-1" data-testid="volume-actions">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button type="button" size="sm" variant="outline">
                      {t('volumeDetail.actions')} <ChevronDown size={14} aria-hidden />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="max-h-96 overflow-y-auto">
                    {groupActions(availableActions).map((g, gi) => (
                      <div key={g.id}>
                        {gi > 0 ? <DropdownMenuSeparator /> : null}
                        <DropdownMenuLabel className="text-xs uppercase tracking-wide text-[var(--color-muted-foreground)]">
                          {t(`volumeActions.group.${g.id}`)}
                        </DropdownMenuLabel>
                        {g.items.map((d) => (
                          <DropdownMenuItem
                            key={d.key}
                            onSelect={() => {
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
                          </DropdownMenuItem>
                        ))}
                      </div>
                    ))}
                    {extraActions.length > 0 ? <DropdownMenuSeparator /> : null}
                    {extraActions.map((k) => (
                      <DropdownMenuItem key={k} onSelect={() => void runAction(vol, k, {})}>
                        {volumeActionLabel(t, k, k)}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            ) : null}

            {showRestoreProgress ? (
              <Card data-testid="restore-progress">
                <CardHeader>
                  <CardTitle>{t('volumeDetail.restoreProgress')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-3">
                    <div className="flex-1">
                      <UsageBar used={restorePercent} total={100} />
                    </div>
                    <span className="w-12 text-right text-sm font-medium tabular-nums">{restorePercent}%</span>
                  </div>
                  {restoreError ? (
                    <p className="mt-2 text-xs text-red-600 dark:text-red-400">{restoreError}</p>
                  ) : null}
                </CardContent>
              </Card>
            ) : null}

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

            <Card data-testid="snapshots-panel">
              <CardHeader className="flex-row items-center justify-between space-y-0">
                <CardTitle className="flex items-center gap-2">
                  <Camera size={16} /> {t('volumeDetail.snapshots')}
                </CardTitle>
                <div className="flex items-center gap-3">
                  <label className="flex items-center gap-1.5 text-xs text-[var(--color-muted-foreground)]">
                    <input
                      type="checkbox"
                      data-testid="show-system-snapshots"
                      checked={showSystemSnapshots}
                      onChange={(e) => setShowSystemSnapshots(e.target.checked)}
                    />
                    {t('volumeDetail.showSystem')}
                  </label>
                  <Button type="button" size="sm" variant="outline" disabled={snapLoading} onClick={() => void refreshSnapshots(vol)}>
                    {t('common.refresh')}
                  </Button>
                  {(hasAction(vol, 'snapshotCRCreate') || hasAction(vol, 'snapshotCreate')) && canMutate ? (
                    <Button type="button" size="sm" data-testid="snapshot-create" onClick={() => setSnapCreateOpen(true)}>
                      {t('volumeDetail.createSnapshot')}
                    </Button>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent>
                <SnapshotTree
                  snapshots={snapshots}
                  showSystem={showSystemSnapshots}
                  labels={{
                    volumeHead: t('volumeDetail.volumeHead'),
                    start: t('volumeDetail.start'),
                    systemTag: t('volumeDetail.systemTag'),
                    empty: t('volumeDetail.noSnapshots'),
                    created: t('common.created'),
                    size: t('common.size'),
                  }}
                  renderActions={(s) => {
                    const isSystemSnap = s.usercreated === false || s.removed === true
                    return (
                      <>
                        {snapPref('Revert') && !isSystemSnap ? (
                          <Button type="button" size="sm" variant="outline" disabled={!canMutate} onClick={() => void runAction(vol, snapPref('Revert')!, { name: s.name })}>
                            {t('volumeDetail.revert')}
                          </Button>
                        ) : null}
                        {hasAction(vol, 'snapshotBackup') && !isSystemSnap ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={!canMutate}
                            onClick={() => {
                              if (backupTargetNames.length === 0) {
                                // No configured targets: keep the legacy empty-target behavior.
                                void runAction(vol, 'snapshotBackup', { name: s.name, backupTargetName: '', labels: {} })
                                return
                              }
                              setBackupTargetName(backupTargetNames[0] ?? '')
                              setBackupMode('incremental')
                              setBackupSnap(s.name)
                            }}
                          >
                            {t('volumeDetail.backup')}
                          </Button>
                        ) : null}
                        {snapPref('Delete') ? (
                          <Button type="button" size="sm" variant="ghost" disabled={!canMutate} onClick={() => setDeleteSnapshot(s.name)}>
                            {t('common.delete')}
                          </Button>
                        ) : null}
                      </>
                    )
                  }}
                />
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
                      {canMutate && hasAction(vol, 'replicaRemove') ? <TH className="text-right" /> : null}
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
                        {canMutate && hasAction(vol, 'replicaRemove') ? (
                          <TD className="text-right">
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              aria-label={t('volumeActions.removeReplica')}
                              onClick={() => setRemoveReplica(r.name ?? null)}
                            >
                              <Trash2 size={14} aria-hidden />
                            </Button>
                          </TD>
                        ) : null}
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
                {(() => {
                  const rows = attachmentTicketRows(attachments as Array<Record<string, unknown>>)
                  if (rows.length === 0) {
                    return (
                      <p className="text-sm text-[var(--color-muted-foreground)]">
                        {t('volumeDetail.noAttachments')}
                      </p>
                    )
                  }
                  return (
                    <Table>
                      <THead>
                        <TR>
                          <TH>{t('volumeDetail.attachType')}</TH>
                          <TH>{t('common.node')}</TH>
                          <TH>{t('volumeDetail.satisfied')}</TH>
                          <TH>{t('volumeDetail.attachId')}</TH>
                        </TR>
                      </THead>
                      <TBody>
                        {rows.map((r) => (
                          <TR key={r.id}>
                            <TD>{r.type ?? '—'}</TD>
                            <TD>
                              {r.nodeID ? (
                                <Link
                                  to={`/nodes/${encodeURIComponent(r.nodeID)}`}
                                  className="text-[var(--color-primary)] hover:underline"
                                >
                                  {r.nodeID}
                                </Link>
                              ) : (
                                '—'
                              )}
                            </TD>
                            <TD>
                              <Badge tone={r.satisfied ? 'success' : 'warning'}>
                                {r.satisfied ? t('common.yes') : t('common.no')}
                              </Badge>
                            </TD>
                            <TD className="max-w-[16rem] truncate font-mono text-xs" title={r.attachmentID}>
                              {r.attachmentID ?? r.id}
                            </TD>
                          </TR>
                        ))}
                      </TBody>
                    </Table>
                  )
                })()}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('volumeDetail.conditions')}</CardTitle>
              </CardHeader>
              <CardContent>
                {toConditionArray(vol.conditions).length ? (
                  <Table>
                    <THead>
                      <TR>
                        <TH>{t('volumeDetail.conditionType')}</TH>
                        <TH>{t('common.status')}</TH>
                        <TH>{t('common.message')}</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {toConditionArray(vol.conditions).map((c, i) => (
                        <TR key={i}>
                          <TD className="font-medium">{c.type}</TD>
                          <TD>
                            <Badge tone={stateTone(c.status)}>{c.status}</Badge>
                          </TD>
                          <TD className="text-[var(--color-muted-foreground)]">{c.message || '—'}</TD>
                        </TR>
                      ))}
                    </TBody>
                  </Table>
                ) : (
                  <p className="text-sm text-[var(--color-muted-foreground)]">{t('volumeDetail.noConditions')}</p>
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
                void runAction(
                  vol,
                  hasAction(vol, 'snapshotCRCreate') ? 'snapshotCRCreate' : 'snapshotCreate',
                  { name: snapName },
                ).then(() => setSnapCreateOpen(false))
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
        <Select
          value={jobName}
          onChange={(e) => setJobName(e.target.value)}
          aria-label={t('volumeDetail.selectJob')}
        >
          <option value="">{t('volumeDetail.selectJob')}</option>
          {(jobsQ.data ?? []).map((j) => (
            <option key={j.name} value={j.name}>
              {j.name} ({j.task})
            </option>
          ))}
        </Select>
      </Dialog>

      <Dialog
        open={Boolean(backupSnap)}
        onOpenChange={(v) => !v && setBackupSnap(null)}
        title={t('volumeDetail.backup')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setBackupSnap(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              data-testid="snapshot-backup-confirm"
              disabled={!vol || !backupSnap}
              onClick={() => {
                if (!vol || !backupSnap) return
                void runAction(vol, 'snapshotBackup', {
                  name: backupSnap,
                  backupTargetName,
                  backupMode,
                  labels: {},
                }).then(() => setBackupSnap(null))
              }}
            >
              {t('volumeDetail.backup')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <label className="block space-y-1 text-sm">
            <span className="text-[var(--color-muted-foreground)]">{t('volumeDetail.backupTarget')}</span>
            <Select
              value={backupTargetName}
              onChange={(e) => setBackupTargetName(e.target.value)}
              aria-label={t('volumeDetail.backupTarget')}
              data-testid="backup-target-select"
            >
              {backupTargetNames.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </Select>
          </label>
          <label className="block space-y-1 text-sm">
            <span className="text-[var(--color-muted-foreground)]">{t('volumeDetail.backupMode')}</span>
            <Select
              value={backupMode}
              onChange={(e) => setBackupMode(e.target.value as 'full' | 'incremental')}
              aria-label={t('volumeDetail.backupMode')}
              data-testid="backup-mode-select"
            >
              <option value="incremental">{t('volumeDetail.incremental')}</option>
              <option value="full">{t('volumeDetail.full')}</option>
            </Select>
          </label>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(removeReplica)}
        onOpenChange={(v) => !v && setRemoveReplica(null)}
        title={t('volumeActions.removeReplica')}
        description={removeReplica ? t('volumeDetail.removeReplicaConfirm', { name: removeReplica }) : ''}
        confirmLabel={t('volumeActions.removeReplica')}
        destructive
        onConfirm={async () => {
          if (vol && removeReplica) await runAction(vol, 'replicaRemove', { name: removeReplica })
          setRemoveReplica(null)
        }}
      />

      <ConfirmDialog
        open={Boolean(deleteSnapshot)}
        onOpenChange={(v) => !v && setDeleteSnapshot(null)}
        title={t('volumeDetail.deleteSnapshot')}
        description={deleteSnapshot ? t('volumeDetail.deleteSnapshotConfirm', { name: deleteSnapshot }) : ''}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={async () => {
          if (vol && deleteSnapshot) await runAction(vol, snapPref('Delete') ?? 'snapshotDelete', { name: deleteSnapshot })
          setDeleteSnapshot(null)
        }}
      />
    </div>
  )
}
