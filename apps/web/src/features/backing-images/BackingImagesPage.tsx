import { useMemo, useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useBackingImages,
  useCreateBackingImage,
  useDeleteBackingImage,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { formatBytes, type BackingImage } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function BackingImagesPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useBackingImages()
  const createMut = useCreateBackingImage()
  const delMut = useDeleteBackingImage()
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
        cell: ({ row }) =>
          canMutate ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-label={t('common.delete')}
              onClick={() => setDeleteTarget(row.original)}
            >
              <Trash2 size={14} aria-hidden />
            </Button>
          ) : null,
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
