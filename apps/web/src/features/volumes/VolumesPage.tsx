import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef, RowSelectionState } from '@tanstack/react-table'
import {
  useBackingImages,
  useCreateVolume,
  useDeleteVolume,
  useEngineImages,
  useNodes,
  useVolumeAction,
  useVolumes,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { formatBytes, hasAction, parseSizeToBytes, type Volume } from '@/api/longhorn'
import { ColumnPicker } from '@/components/data/ColumnPicker'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { DensityToggle } from '@/components/data/DensityToggle'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { SavedViews } from '@/components/data/SavedViews'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { resolveColumns, usePreferences } from '@/store/preferences'
import { ActionFormDialog } from './ActionFormDialog'
import {
  BULK_ACTIONS,
  VOLUME_ACTION_DEFS,
  volumeActionLabel,
  type VolumeActionDef,
} from './volumeActions'

const VOLUMES_TABLE_ID = 'volumes'

const VOLUME_COLUMN_IDS = [
  'name',
  'state',
  'robustness',
  'size',
  'replicas',
  'pvc',
  'engine',
  'actions',
] as const

const REPLICA_AUTO_BALANCE_OPTS = ['ignored', 'disabled', 'least-effort', 'best-effort'] as const
const SNAPSHOT_DATA_INTEGRITY_OPTS = ['ignored', 'disabled', 'enabled', 'fast-check'] as const
const ANTI_AFFINITY_OPTS = ['ignored', 'enabled', 'disabled'] as const
const UNMAP_MARK_OPTS = ['ignored', 'disabled', 'enabled'] as const

/** Split a comma/space separated tag string into a trimmed, de-duplicated array. */
function parseTags(input: string): string[] {
  return Array.from(
    new Set(
      input
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
    ),
  )
}

export function VolumesPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useVolumes()
  const nodesQ = useNodes()
  const imagesQ = useEngineImages()
  const backingImagesQ = useBackingImages()
  const createMut = useCreateVolume()
  const deleteMut = useDeleteVolume()
  const actionMut = useVolumeAction()
  const columnPrefs = usePreferences((s) => s.columnPrefs)

  const volumeColumns = useMemo(
    () =>
      VOLUME_COLUMN_IDS.map((id) => ({
        id,
        label: t(`volumes.columns.${id}`),
      })),
    [t],
  )

  const [filter, setFilter] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [size, setSize] = useState('10Gi')
  const [replicas, setReplicas] = useState('3')
  const [frontend, setFrontend] = useState('blockdev')
  const [accessMode, setAccessMode] = useState('rwo')
  const [dataLocality, setDataLocality] = useState('disabled')
  const [standby, setStandby] = useState(false)
  const [fromBackup, setFromBackup] = useState('')
  // Advanced options
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [backingImage, setBackingImage] = useState('')
  const [dataSourceType, setDataSourceType] = useState('')
  const [dataSourceVolume, setDataSourceVolume] = useState('')
  const [dataSourceSnapshot, setDataSourceSnapshot] = useState('')
  const [encrypted, setEncrypted] = useState(false)
  const [nodeSelector, setNodeSelector] = useState('')
  const [diskSelector, setDiskSelector] = useState('')
  const [replicaAutoBalance, setReplicaAutoBalance] = useState('ignored')
  const [snapshotDataIntegrity, setSnapshotDataIntegrity] = useState('ignored')
  const [replicaSoftAntiAffinity, setReplicaSoftAntiAffinity] = useState('ignored')
  const [replicaZoneSoftAntiAffinity, setReplicaZoneSoftAntiAffinity] = useState('ignored')
  const [replicaDiskSoftAntiAffinity, setReplicaDiskSoftAntiAffinity] = useState('ignored')
  const [revisionCounterDisabled, setRevisionCounterDisabled] = useState(false)
  const [unmapMarkSnapChainRemoved, setUnmapMarkSnapChainRemoved] = useState('ignored')
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Volume | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [selected, setSelected] = useState<RowSelectionState>({})
  const [bulkKey, setBulkKey] = useState<string | null>(null)
  const [bulkValue, setBulkValue] = useState('')
  const [bulkHost, setBulkHost] = useState('')
  const [actionDef, setActionDef] = useState<VolumeActionDef | null>(null)
  const [actionVol, setActionVol] = useState<Volume | null>(null)

  const hosts = (nodesQ.data ?? []).map((n) => n.name)
  const images = (imagesQ.data ?? []).map((i) => i.image ?? i.name).filter(Boolean) as string[]
  const backingImages = (backingImagesQ.data ?? []).map((b) => b.name).filter(Boolean) as string[]

  // Map the ColumnPicker/preferences visible-column set into TanStack VisibilityState.
  const columnVisibility = useMemo(() => {
    const visible = new Set(resolveColumns(columnPrefs, VOLUMES_TABLE_ID, [...VOLUME_COLUMN_IDS]))
    return Object.fromEntries(VOLUME_COLUMN_IDS.map((id) => [id, visible.has(id)])) as Record<
      string,
      boolean
    >
  }, [columnPrefs])

  const data = q.data ?? []
  const selectedVols = data.filter((v) => selected[v.name])

  const columns = useMemo<ColumnDef<Volume, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (v) => v.name ?? '',
        header: t('volumes.columns.name'),
        cell: ({ row }) => {
          const v = row.original
          return (
            <>
              <Link
                to={`/volumes/${encodeURIComponent(v.name)}`}
                className="font-medium text-[var(--color-primary)] hover:underline"
              >
                {v.name}
              </Link>
              {v.standby ? (
                <Badge tone="warning" className="ml-1">
                  DR
                </Badge>
              ) : null}
            </>
          )
        },
      },
      {
        id: 'state',
        accessorFn: (v) => v.state ?? '',
        header: t('volumes.columns.state'),
        cell: ({ row }) => <Badge tone={stateTone(row.original.state)}>{row.original.state ?? '—'}</Badge>,
      },
      {
        id: 'robustness',
        accessorFn: (v) => v.robustness ?? '',
        header: t('volumes.columns.robustness'),
        cell: ({ row }) => (
          <Badge tone={stateTone(row.original.robustness)}>{row.original.robustness ?? '—'}</Badge>
        ),
      },
      {
        id: 'size',
        accessorFn: (v) => Number(v.size ?? 0),
        header: t('volumes.columns.size'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => formatBytes(row.original.size),
      },
      {
        id: 'replicas',
        accessorFn: (v) => v.numberOfReplicas ?? 0,
        header: t('volumes.columns.replicas'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => row.original.numberOfReplicas ?? '—',
      },
      {
        id: 'pvc',
        accessorFn: (v) =>
          v.kubernetesStatus?.pvcName
            ? `${v.kubernetesStatus.namespace}/${v.kubernetesStatus.pvcName}`
            : '',
        header: t('volumes.columns.pvc'),
        meta: { className: 'max-w-[8rem] truncate text-xs' },
        cell: ({ getValue }) => (getValue() as string) || '—',
      },
      {
        id: 'engine',
        accessorFn: (v) => v.dataEngine ?? 'v1',
        header: t('volumes.columns.engine'),
      },
      {
        id: 'actions',
        header: t('volumes.columns.actions'),
        enableSorting: false,
        meta: { headerClassName: 'text-right' },
        cell: ({ row }) => {
          const v = row.original
          return (
            <div className="flex flex-wrap justify-end gap-1">
              {VOLUME_ACTION_DEFS.filter(
                (d) =>
                  (d.priority === 'P0' || d.priority === 'P1') &&
                  hasAction(v, d.key) &&
                  d.key !== 'snapshotCreate',
              )
                .slice(0, 4)
                .map((d) => (
                  <Button
                    key={d.key}
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={!canMutate}
                    onClick={() => {
                      setActionVol(v)
                      setActionDef(d as VolumeActionDef)
                    }}
                  >
                    {volumeActionLabel(t, d.key, d.label)}
                  </Button>
                ))}
              <Link
                to={`/volumes/${encodeURIComponent(v.name)}`}
                className="inline-flex h-8 items-center rounded-md border border-[var(--color-border)] px-2 text-xs"
              >
                {t('common.detail')}
              </Link>
              {canMutate ? (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  aria-label={t('volumes.deleteAria', { name: v.name })}
                  title={t('volumes.deleteTitle', { name: v.name })}
                  onClick={() => setDeleteTarget(v)}
                >
                  <Trash2 size={14} aria-hidden />
                </Button>
              ) : null}
            </div>
          )
        },
      },
    ],
    [t, canMutate],
  )

  function resetCreateForm() {
    setName('')
    setFromBackup('')
    setStandby(false)
    setShowAdvanced(false)
    setBackingImage('')
    setDataSourceType('')
    setDataSourceVolume('')
    setDataSourceSnapshot('')
    setEncrypted(false)
    setNodeSelector('')
    setDiskSelector('')
    setReplicaAutoBalance('ignored')
    setSnapshotDataIntegrity('ignored')
    setReplicaSoftAntiAffinity('ignored')
    setReplicaZoneSoftAntiAffinity('ignored')
    setReplicaDiskSoftAntiAffinity('ignored')
    setRevisionCounterDisabled(false)
    setUnmapMarkSnapChainRemoved('ignored')
  }

  async function onCreate() {
    setFormError(null)
    try {
      const sizeBytes = parseSizeToBytes(size)
      const body: Record<string, unknown> = {
        name,
        size: sizeBytes,
        numberOfReplicas: Number(replicas) || 3,
        frontend,
        dataLocality,
        accessMode,
        standby,
      }
      // Only include advanced fields the user actually set, to avoid over-constraining.
      if (fromBackup) body.fromBackup = fromBackup
      if (backingImage) body.backingImage = backingImage
      // dataSource: vol://<name> or snap://<vol>/<snap>
      if (dataSourceType === 'volume' && dataSourceVolume) {
        body.dataSource = `vol://${dataSourceVolume}`
      } else if (dataSourceType === 'snapshot' && dataSourceVolume && dataSourceSnapshot) {
        body.dataSource = `snap://${dataSourceVolume}/${dataSourceSnapshot}`
      }
      if (encrypted) body.encrypted = true
      const nodeTags = parseTags(nodeSelector)
      if (nodeTags.length) body.nodeSelector = nodeTags
      const diskTags = parseTags(diskSelector)
      if (diskTags.length) body.diskSelector = diskTags
      if (replicaAutoBalance !== 'ignored') body.replicaAutoBalance = replicaAutoBalance
      if (snapshotDataIntegrity !== 'ignored') body.snapshotDataIntegrity = snapshotDataIntegrity
      if (replicaSoftAntiAffinity !== 'ignored') body.replicaSoftAntiAffinity = replicaSoftAntiAffinity
      if (replicaZoneSoftAntiAffinity !== 'ignored')
        body.replicaZoneSoftAntiAffinity = replicaZoneSoftAntiAffinity
      if (replicaDiskSoftAntiAffinity !== 'ignored')
        body.replicaDiskSoftAntiAffinity = replicaDiskSoftAntiAffinity
      if (revisionCounterDisabled) body.revisionCounterDisabled = true
      if (unmapMarkSnapChainRemoved !== 'ignored')
        body.unmapMarkSnapChainRemoved = unmapMarkSnapChainRemoved
      await createMut.mutateAsync(body)
      setCreateOpen(false)
      resetCreateForm()
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  async function runBulk() {
    if (!bulkKey) return
    setActionError(null)
    try {
      for (const vol of selectedVols) {
        if (bulkKey === 'delete') {
          await deleteMut.mutateAsync(vol)
          continue
        }
        if (!hasAction(vol, bulkKey) && bulkKey !== 'attach') continue
        const params: Record<string, unknown> = {}
        if (bulkKey === 'attach') {
          params.hostId = bulkHost || hosts[0]
          params.disableFrontend = false
          params.attachedBy = ''
          params.attacherType = ''
          params.attachmentID = ''
        } else if (bulkKey === 'detach') {
          params.forceAttachment = false
        } else if (bulkKey === 'updateReplicaCount') {
          params.replicaCount = Number(bulkValue) || 3
        } else if (bulkKey === 'updateDataLocality') {
          params.dataLocality = bulkValue || 'disabled'
        } else if (bulkKey === 'updateAccessMode') {
          params.accessMode = bulkValue || 'rwo'
        } else if (bulkKey === 'engineUpgrade') {
          params.image = bulkValue
        } else if (bulkKey === 'activate') {
          params.frontend = 'blockdev'
        } else if (bulkKey === 'snapshotCreate') {
          params.name = ''
        }
        await actionMut.mutateAsync({ vol, action: bulkKey, params })
      }
      setSelected({})
      setBulkKey(null)
      await q.refetch()
    } catch (e) {
      setActionError(e instanceof Error ? e.message : t('volumeActions.bulkFailed'))
    }
  }

  return (
    <div data-testid="volumes-page">
      <PageHeader
        title={t('volumes.title')}
        description={t('volumes.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            {canMutate ? (
              <Button type="button" size="sm" onClick={() => setCreateOpen(true)} data-testid="create-volume">
                <Plus size={14} /> {t('common.create')}
              </Button>
            ) : null}
          </>
        }
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          placeholder={t('volumes.filterPlaceholder')}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-md"
          data-testid="volume-filter"
        />
        <DensityToggle />
        <ColumnPicker tableId={VOLUMES_TABLE_ID} allColumns={volumeColumns} />
        <SavedViews
          tableId={VOLUMES_TABLE_ID}
          filters={{ q: filter }}
          allColumnIds={[...VOLUME_COLUMN_IDS]}
          onApplyFilters={(f) => setFilter(f.q ?? '')}
        />
        {canMutate && selectedVols.length > 0 ? (
          <div className="flex flex-wrap gap-1" data-testid="bulk-actions">
            <Badge tone="info">{t('volumes.selected', { count: selectedVols.length })}</Badge>
            {BULK_ACTIONS.map((a) => (
              <Button
                key={a.key}
                type="button"
                size="sm"
                variant={'destructive' in a && a.destructive ? 'destructive' : 'outline'}
                onClick={() => {
                  setBulkKey(a.key)
                  setBulkValue('')
                  setBulkHost(hosts[0] ?? '')
                }}
              >
                {t(a.labelKey, { defaultValue: a.label })}
              </Button>
            ))}
            <Button type="button" size="sm" variant="outline" onClick={() => setBulkKey('attach')}>
              {t('volumes.bulkAttach')}
            </Button>
          </div>
        ) : null}
      </div>

      {actionError ? (
        <Alert tone="danger" className="mb-3">
          {actionError}
        </Alert>
      ) : null}

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!data.length}
        emptyTitle={t('volumes.empty')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="volumes-table"
          columns={columns}
          data={data}
          getRowId={(v) => v.name}
          columnVisibility={columnVisibility}
          globalFilter={filter}
          enableExport
          exportName="highland-volumes"
          enableSelection
          rowSelection={selected}
          onRowSelectionChange={setSelected}
        />
      </QueryState>

      <Dialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title={t('volumes.createVolume')}
        description={t('volumes.createDescription')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void onCreate()} disabled={!name || createMut.isPending} data-testid="create-volume-submit">
              {t('common.create')}
            </Button>
          </>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block space-y-1 text-sm sm:col-span-2">
            <span className="font-medium">{t('common.name')}</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} data-testid="create-volume-name" />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('common.size')}</span>
            <Input value={size} onChange={(e) => setSize(e.target.value)} data-testid="create-volume-size" />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('volumes.columns.replicas')}</span>
            <Input value={replicas} onChange={(e) => setReplicas(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('volumes.frontend')}</span>
            <select
              className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
              value={frontend}
              onChange={(e) => setFrontend(e.target.value)}
            >
              {['blockdev', 'iscsi', 'nvmf', 'ublk', ''].map((f) => (
                <option key={f || 'none'} value={f}>
                  {f || t('volumes.emptyFrontend')}
                </option>
              ))}
            </select>
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('volumes.accessMode')}</span>
            <select
              className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
              value={accessMode}
              onChange={(e) => setAccessMode(e.target.value)}
            >
              <option value="rwo">rwo</option>
              <option value="rwx">rwx</option>
            </select>
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('volumes.dataLocality')}</span>
            <select
              className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
              value={dataLocality}
              onChange={(e) => setDataLocality(e.target.value)}
            >
              <option value="disabled">disabled</option>
              <option value="best-effort">best-effort</option>
              <option value="strict-local">strict-local</option>
            </select>
          </label>
          <label className="block space-y-1 text-sm sm:col-span-2">
            <span className="font-medium">{t('volumes.fromBackupLabel')}</span>
            <Input value={fromBackup} onChange={(e) => setFromBackup(e.target.value)} placeholder="backup://..." />
          </label>
          <label className="flex items-center gap-2 text-sm sm:col-span-2">
            <input type="checkbox" checked={standby} onChange={(e) => setStandby(e.target.checked)} />
            {t('volumes.createStandby')}
          </label>

          <div className="sm:col-span-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="px-0"
              aria-expanded={showAdvanced}
              onClick={() => setShowAdvanced((v) => !v)}
              data-testid="toggle-advanced"
            >
              {showAdvanced ? '▾' : '▸'} {t('volumes.advancedOptions')}
            </Button>
          </div>

          {showAdvanced ? (
            <div className="grid gap-3 sm:col-span-2 sm:grid-cols-2" data-testid="advanced-options">
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.backingImage')}</span>
                <Select value={backingImage} onChange={(e) => setBackingImage(e.target.value)}>
                  <option value="">{t('volumes.backingImageNone')}</option>
                  {backingImages.map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.dataSourceType')}</span>
                <Select
                  value={dataSourceType}
                  onChange={(e) => {
                    setDataSourceType(e.target.value)
                    setDataSourceVolume('')
                    setDataSourceSnapshot('')
                  }}
                >
                  <option value="">{t('volumes.dataSourceNone')}</option>
                  <option value="volume">{t('volumes.dataSourceVolumeOpt')}</option>
                  <option value="snapshot">{t('volumes.dataSourceSnapshotOpt')}</option>
                </Select>
              </label>
              {dataSourceType ? (
                <label className="block space-y-1 text-sm">
                  <span className="font-medium">{t('volumes.dataSourceVolume')}</span>
                  <Input
                    value={dataSourceVolume}
                    onChange={(e) => setDataSourceVolume(e.target.value)}
                    placeholder={t('volumes.dataSourceVolumePlaceholder')}
                  />
                </label>
              ) : null}
              {dataSourceType === 'snapshot' ? (
                <label className="block space-y-1 text-sm">
                  <span className="font-medium">{t('volumes.dataSourceSnapshot')}</span>
                  <Input
                    value={dataSourceSnapshot}
                    onChange={(e) => setDataSourceSnapshot(e.target.value)}
                    placeholder={t('volumes.dataSourceSnapshotPlaceholder')}
                  />
                </label>
              ) : null}
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.nodeTag')}</span>
                <Input
                  value={nodeSelector}
                  onChange={(e) => setNodeSelector(e.target.value)}
                  placeholder={t('volumes.tagsPlaceholder')}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.diskTag')}</span>
                <Input
                  value={diskSelector}
                  onChange={(e) => setDiskSelector(e.target.value)}
                  placeholder={t('volumes.tagsPlaceholder')}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.replicaAutoBalance')}</span>
                <Select
                  value={replicaAutoBalance}
                  onChange={(e) => setReplicaAutoBalance(e.target.value)}
                >
                  {REPLICA_AUTO_BALANCE_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.snapshotDataIntegrity')}</span>
                <Select
                  value={snapshotDataIntegrity}
                  onChange={(e) => setSnapshotDataIntegrity(e.target.value)}
                >
                  {SNAPSHOT_DATA_INTEGRITY_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.replicaSoftAntiAffinity')}</span>
                <Select
                  value={replicaSoftAntiAffinity}
                  onChange={(e) => setReplicaSoftAntiAffinity(e.target.value)}
                >
                  {ANTI_AFFINITY_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.replicaZoneSoftAntiAffinity')}</span>
                <Select
                  value={replicaZoneSoftAntiAffinity}
                  onChange={(e) => setReplicaZoneSoftAntiAffinity(e.target.value)}
                >
                  {ANTI_AFFINITY_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.replicaDiskSoftAntiAffinity')}</span>
                <Select
                  value={replicaDiskSoftAntiAffinity}
                  onChange={(e) => setReplicaDiskSoftAntiAffinity(e.target.value)}
                >
                  {ANTI_AFFINITY_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">{t('volumes.unmapMarkSnapChainRemoved')}</span>
                <Select
                  value={unmapMarkSnapChainRemoved}
                  onChange={(e) => setUnmapMarkSnapChainRemoved(e.target.value)}
                >
                  {UNMAP_MARK_OPTS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="flex items-center gap-2 text-sm sm:col-span-2">
                <input
                  type="checkbox"
                  checked={encrypted}
                  onChange={(e) => setEncrypted(e.target.checked)}
                />
                {t('volumes.encrypted')}
              </label>
              <label className="flex items-center gap-2 text-sm sm:col-span-2">
                <input
                  type="checkbox"
                  checked={revisionCounterDisabled}
                  onChange={(e) => setRevisionCounterDisabled(e.target.checked)}
                />
                {t('volumes.revisionCounterDisabled')}
              </label>
            </div>
          ) : null}

          {formError ? <Alert tone="danger" className="sm:col-span-2">{formError}</Alert> : null}
        </div>
      </Dialog>

      <Dialog
        open={Boolean(bulkKey)}
        onOpenChange={(v) => !v && setBulkKey(null)}
        title={t('volumes.bulkTitle', { key: bulkKey })}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setBulkKey(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void runBulk()} disabled={actionMut.isPending || deleteMut.isPending}>
              {t('volumes.runOnVolumes', { count: selectedVols.length })}
            </Button>
          </>
        }
      >
        {bulkKey === 'attach' ? (
          <select
            className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
            value={bulkHost}
            onChange={(e) => setBulkHost(e.target.value)}
          >
            {hosts.map((h) => (
              <option key={h} value={h}>
                {h}
              </option>
            ))}
          </select>
        ) : null}
        {bulkKey === 'updateReplicaCount' ? (
          <Input value={bulkValue} onChange={(e) => setBulkValue(e.target.value)} placeholder={t('volumes.replicaCountPlaceholder')} />
        ) : null}
        {bulkKey === 'updateDataLocality' ? (
          <select
            className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
            value={bulkValue}
            onChange={(e) => setBulkValue(e.target.value)}
          >
            <option value="disabled">disabled</option>
            <option value="best-effort">best-effort</option>
            <option value="strict-local">strict-local</option>
          </select>
        ) : null}
        {bulkKey === 'updateAccessMode' ? (
          <select
            className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
            value={bulkValue}
            onChange={(e) => setBulkValue(e.target.value)}
          >
            <option value="rwo">rwo</option>
            <option value="rwx">rwx</option>
          </select>
        ) : null}
        {bulkKey === 'engineUpgrade' ? (
          <Input value={bulkValue} onChange={(e) => setBulkValue(e.target.value)} placeholder={t('volumes.engineImagePlaceholder')} list="eng-images" />
        ) : null}
        <datalist id="eng-images">
          {images.map((i) => (
            <option key={i} value={i} />
          ))}
        </datalist>
        {bulkKey === 'delete' ? (
          <p className="text-sm text-[var(--color-destructive)]">{t('volumes.deleteBulk', { count: selectedVols.length })}</p>
        ) : null}
      </Dialog>

      <ActionFormDialog
        open={Boolean(actionDef && actionVol)}
        onOpenChange={(v) => {
          if (!v) {
            setActionDef(null)
            setActionVol(null)
          }
        }}
        def={actionDef}
        hosts={hosts}
        images={images}
        replicas={(actionVol?.replicas ?? []).map((r) => r.name ?? '').filter(Boolean)}
        loading={actionMut.isPending}
        onSubmit={async (params) => {
          if (!actionVol || !actionDef) return
          setActionError(null)
          try {
            await actionMut.mutateAsync({ vol: actionVol, action: actionDef.key, params })
            await q.refetch()
          } catch (e) {
            setActionError(e instanceof Error ? e.message : t('volumeActions.actionFailed'))
          }
        }}
      />

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('volumes.deleteVolume')}
        description={deleteTarget ? t('volumes.deleteConfirm', { name: deleteTarget.name }) : undefined}
        confirmText={deleteTarget?.name}
        confirmLabel={t('common.delete')}
        destructive
        loading={deleteMut.isPending}
        onConfirm={async () => {
          if (!deleteTarget) return
          await deleteMut.mutateAsync(deleteTarget)
          setDeleteTarget(null)
        }}
      />
    </div>
  )
}
