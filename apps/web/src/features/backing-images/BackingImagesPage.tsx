import { useMemo, useState } from 'react'
import { Archive, Layers, MoreHorizontal, Plus, RefreshCw, Trash2, Undo2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useBackingImageAction,
  useBackingImages,
  useBackupBackingImage,
  useBackupBackingImages,
  useCreateBackingImage,
  useDeleteBackingImage,
  useDeleteBackupBackingImage,
  useRestoreBackupBackingImage,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import {
  formatBytes,
  type BackingImage,
  type BackupBackingImage,
} from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'

/** True when at least one disk holds a ready copy — required before backing up / setting min copies. */
function hasReadyDisk(img: BackingImage): boolean {
  const m = img.diskFileStatusMap
  if (!m) return false
  return Object.values(m).some((d) => (d?.state ?? '').toLowerCase() === 'ready')
}

export function BackingImagesPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useBackingImages()
  const bbiQ = useBackupBackingImages()
  const createMut = useCreateBackingImage()
  const delMut = useDeleteBackingImage()
  const backupMut = useBackupBackingImage()
  const minCopiesMut = useBackingImageAction()
  const restoreMut = useRestoreBackupBackingImage()
  const delBbiMut = useDeleteBackupBackingImage()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [sourceType, setSourceType] = useState('download')
  const [url, setUrl] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BackingImage | null>(null)
  const [uploading, setUploading] = useState(false)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [bulkRows, setBulkRows] = useState<BackingImage[]>([])

  const [backupTarget, setBackupTarget] = useState<BackingImage | null>(null)
  const [minCopiesTarget, setMinCopiesTarget] = useState<BackingImage | null>(null)
  const [minCopies, setMinCopies] = useState('1')
  const [restoreTarget, setRestoreTarget] = useState<BackupBackingImage | null>(null)
  const [restoreEngine, setRestoreEngine] = useState('v1')
  const [deleteBbiTarget, setDeleteBbiTarget] = useState<BackupBackingImage | null>(null)

  const columns = useMemo<ColumnDef<BackingImage, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (img) => img.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'uuid',
        accessorFn: (img) => img.uuid ?? '',
        header: t('backingImages.uuid'),
        meta: { className: 'max-w-[10rem] truncate font-mono text-xs' },
        cell: ({ row }) => row.original.uuid ?? '—',
      },
      {
        id: 'size',
        accessorFn: (img) => Number(img.size ?? 0),
        header: t('common.size'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => formatBytes(row.original.size),
      },
      {
        id: 'minCopies',
        accessorFn: (img) => Number(img.minNumberOfCopies ?? 0),
        header: t('backingImages.minCopies'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => row.original.minNumberOfCopies ?? '—',
      },
      {
        id: 'checksum',
        accessorFn: (img) => img.currentChecksum ?? '',
        header: t('backingImages.checksum'),
        meta: { className: 'max-w-[12rem] truncate font-mono text-xs' },
        cell: ({ row }) => row.original.currentChecksum ?? '—',
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        meta: { headerClassName: 'text-right', className: 'text-right' },
        cell: ({ row }) => {
          if (!canMutate) return null
          const img = row.original
          const ready = hasReadyDisk(img)
          return (
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button type="button" size="sm" variant="ghost" aria-label={t('common.rowActions')}>
                    <MoreHorizontal size={16} aria-hidden />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    disabled={!ready}
                    title={ready ? undefined : t('backingImages.needsReadyDisk')}
                    onSelect={() => setBackupTarget(img)}
                  >
                    <Archive size={14} aria-hidden /> {t('backingImages.backUp')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    disabled={!ready}
                    title={ready ? undefined : t('backingImages.needsReadyDisk')}
                    onSelect={() => {
                      setMinCopiesTarget(img)
                      setMinCopies(String(img.minNumberOfCopies ?? 1))
                    }}
                  >
                    <Layers size={14} aria-hidden /> {t('backingImages.minCopies')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onSelect={() => setDeleteTarget(img)}>
                    <Trash2 size={14} aria-hidden /> {t('common.delete')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        },
      },
    ],
    [t, canMutate],
  )

  const bbiColumns = useMemo<ColumnDef<BackupBackingImage, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (b) => b.backingImageName ?? b.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.backingImageName ?? row.original.name,
      },
      {
        id: 'state',
        accessorFn: (b) => b.state ?? '',
        header: t('common.state'),
        cell: ({ row }) =>
          row.original.state ? (
            <Badge tone={stateTone(row.original.state)}>{row.original.state}</Badge>
          ) : (
            '—'
          ),
      },
      {
        id: 'backupTarget',
        accessorFn: (b) => b.backupTargetName ?? '',
        header: t('backingImages.backupTarget'),
        cell: ({ row }) => row.original.backupTargetName ?? '—',
      },
      {
        id: 'size',
        accessorFn: (b) => Number(b.size ?? 0),
        header: t('common.size'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => formatBytes(row.original.size),
      },
      {
        id: 'url',
        accessorFn: (b) => b.url ?? '',
        header: t('backingImages.backupUrl'),
        meta: { className: 'max-w-[14rem] truncate font-mono text-xs' },
        cell: ({ row }) => row.original.url ?? '—',
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        meta: { headerClassName: 'text-right', className: 'text-right' },
        cell: ({ row }) => {
          if (!canMutate) return null
          const bbi = row.original
          const restorable = (bbi.state ?? '').toLowerCase() === 'completed'
          return (
            <div className="flex justify-end gap-1">
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={!restorable}
                title={restorable ? undefined : t('backingImages.onlyCompletedRestore')}
                aria-label={t('common.restore')}
                onClick={() => {
                  setRestoreTarget(bbi)
                  setRestoreEngine('v1')
                }}
              >
                <Undo2 size={14} aria-hidden />
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                onClick={() => setDeleteBbiTarget(bbi)}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          )
        },
      },
    ],
    [t, canMutate],
  )

  async function createOrUpload() {
    setError(null)
    try {
      if (sourceType === 'upload' && file) {
        setUploading(true)
        // Create metadata first, then stream file to proxy upload path when actions available
        const created = await createMut.mutateAsync({
          name,
          sourceType: 'upload',
          parameters: {},
        })
        const uploadUrl =
          (created as BackingImage).actions?.upload ||
          (created as BackingImage).links?.upload ||
          `/api/v1/lh/backingimages/${encodeURIComponent(name)}`
        const res = await fetch(uploadUrl, {
          method: 'POST',
          credentials: 'include',
          body: file,
          headers: {
            'Content-Type': 'application/octet-stream',
          },
        })
        if (!res.ok) {
          throw new Error(t('admin.createFailed'))
        }
        setOpen(false)
        await q.refetch()
        return
      }
      await createMut.mutateAsync({
        name,
        sourceType,
        parameters: sourceType === 'download' ? { url } : {},
      })
      setOpen(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    } finally {
      setUploading(false)
    }
  }

  return (
    <div data-testid="backing-images-page">
      <PageHeader
        title={t('backingImages.title')}
        description={t('backingImages.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            {canMutate ? (
              <Button type="button" size="sm" onClick={() => setOpen(true)}>
                <Plus size={14} /> {t('common.create')}
              </Button>
            ) : null}
          </>
        }
      />
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('backingImages.empty')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="backing-images-table"
          columns={columns}
          data={q.data ?? []}
          getRowId={(img) => img.id ?? img.name}
          tableId="backing-images"
          searchable
          enableExport
          exportName="highland-backing-images"
          enableSelection
          bulkActions={(sel) =>
            canMutate ? (
              <Button
                type="button"
                size="sm"
                variant="destructive"
                className="h-7 gap-1 text-xs"
                onClick={() => {
                  setBulkRows(sel)
                  setBulkDeleteOpen(true)
                }}
              >
                <Trash2 size={14} aria-hidden /> {t('common.delete')}
              </Button>
            ) : null
          }
        />
      </QueryState>

      <section className="mt-8" data-testid="backup-backing-images-section">
        <h2 className="mb-3 text-lg font-semibold">{t('backingImages.backupSection')}</h2>
        <QueryState
          isLoading={bbiQ.isLoading}
          error={bbiQ.error as Error | null}
          isEmpty={!bbiQ.data?.length}
          emptyTitle={t('backingImages.backupEmpty')}
          onRetry={() => void bbiQ.refetch()}
        >
          <DataTable
            data-testid="backup-backing-images-table"
            columns={bbiColumns}
            data={bbiQ.data ?? []}
            getRowId={(b) => b.id ?? b.name}
            tableId="backup-backing-images"
            searchable
            enableExport
            exportName="highland-backup-backing-images"
          />
        </QueryState>
      </section>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('backingImages.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" disabled={!name || uploading} onClick={() => void createOrUpload()}>
              {uploading ? t('common.uploading') : t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input placeholder={t('common.name')} value={name} onChange={(e) => setName(e.target.value)} />
          <Select value={sourceType} onChange={(e) => setSourceType(e.target.value)}>
            <option value="download">download</option>
            <option value="upload">{t('backingImages.uploadOption')}</option>
            <option value="clone-from-volume">clone-from-volume</option>
          </Select>
          {sourceType === 'download' ? (
            <Input
              placeholder={t('backingImages.imageUrl')}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          ) : null}
          {sourceType === 'upload' ? (
            <Input
              type="file"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          ) : null}
        </div>
      </Dialog>

      <Dialog
        open={Boolean(minCopiesTarget)}
        onOpenChange={(v) => !v && setMinCopiesTarget(null)}
        title={t('backingImages.minCopiesTitle')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setMinCopiesTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              disabled={minCopiesMut.isPending || !minCopies}
              onClick={async () => {
                if (!minCopiesTarget) return
                await minCopiesMut.mutateAsync({
                  img: minCopiesTarget,
                  action: 'updateMinNumberOfCopies',
                  params: { minNumberOfCopies: Number(minCopies) },
                })
                setMinCopiesTarget(null)
              }}
            >
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <label className="text-sm text-[var(--color-muted-foreground)]">
            {t('backingImages.minCopies')}
          </label>
          <Input
            type="number"
            min={1}
            value={minCopies}
            onChange={(e) => setMinCopies(e.target.value)}
          />
        </div>
      </Dialog>

      <Dialog
        open={Boolean(restoreTarget)}
        onOpenChange={(v) => !v && setRestoreTarget(null)}
        title={t('backingImages.restoreTitle')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setRestoreTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              disabled={restoreMut.isPending}
              onClick={async () => {
                if (!restoreTarget) return
                await restoreMut.mutateAsync({
                  bbi: restoreTarget,
                  params: {
                    secret: restoreTarget.secret ?? '',
                    secretNamespace: restoreTarget.secretNamespace ?? '',
                    dataEngine: restoreEngine,
                  },
                })
                setRestoreTarget(null)
              }}
            >
              {t('common.restore')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input
            disabled
            value={restoreTarget?.backingImageName ?? restoreTarget?.name ?? ''}
          />
          <div className="space-y-2">
            <label className="text-sm text-[var(--color-muted-foreground)]">
              {t('backingImages.dataEngine')}
            </label>
            <Select value={restoreEngine} onChange={(e) => setRestoreEngine(e.target.value)}>
              <option value="v1">v1</option>
              <option value="v2">v2</option>
            </Select>
          </div>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(backupTarget)}
        onOpenChange={(v) => !v && setBackupTarget(null)}
        title={t('backingImages.backUpTitle')}
        confirmText={backupTarget?.name}
        confirmLabel={t('backingImages.backUp')}
        loading={backupMut.isPending}
        onConfirm={async () => {
          if (!backupTarget) return
          await backupMut.mutateAsync(backupTarget)
          setBackupTarget(null)
        }}
      />

      <ConfirmDialog
        open={Boolean(deleteBbiTarget)}
        onOpenChange={(v) => !v && setDeleteBbiTarget(null)}
        title={t('backingImages.deleteBackup')}
        confirmText={deleteBbiTarget?.backingImageName ?? deleteBbiTarget?.name}
        destructive
        confirmLabel={t('common.delete')}
        loading={delBbiMut.isPending}
        onConfirm={async () => {
          if (!deleteBbiTarget) return
          await delBbiMut.mutateAsync(deleteBbiTarget)
          setDeleteBbiTarget(null)
        }}
      />

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('backingImages.delete')}
        confirmText={deleteTarget?.name}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          if (!deleteTarget) return
          await delMut.mutateAsync(deleteTarget)
          setDeleteTarget(null)
        }}
      />

      <ConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={(v) => !v && setBulkDeleteOpen(false)}
        title={t('backingImages.delete')}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          for (const img of bulkRows) {
            await delMut.mutateAsync(img)
          }
          setBulkDeleteOpen(false)
          setBulkRows([])
        }}
      />
    </div>
  )
}
