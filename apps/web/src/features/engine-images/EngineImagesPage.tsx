import { useMemo, useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useCreateEngineImage,
  useDeleteEngineImage,
  useEngineImages,
} from '@/api/hooks'
import type { EngineImage } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function EngineImagesPage() {
  const { t } = useAppTranslation()
  const q = useEngineImages()
  const createMut = useCreateEngineImage()
  const delMut = useDeleteEngineImage()
  const [open, setOpen] = useState(false)
  const [image, setImage] = useState('longhornio/longhorn-engine:v1.12.0')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<EngineImage | null>(null)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [bulkRows, setBulkRows] = useState<EngineImage[]>([])

  const columns = useMemo<ColumnDef<EngineImage, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (img) => img.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'image',
        accessorFn: (img) => img.image ?? '',
        header: t('common.image'),
        meta: { className: 'max-w-xs truncate font-mono text-xs' },
        cell: ({ row }) => row.original.image,
      },
      {
        id: 'state',
        accessorFn: (img) => img.state ?? '',
        header: t('common.state'),
        cell: ({ row }) => (
          <Badge tone={stateTone(row.original.state)}>{row.original.state ?? '—'}</Badge>
        ),
      },
      {
        id: 'default',
        accessorFn: (img) => (img.default ? 1 : 0),
        header: t('common.default'),
        cell: ({ row }) => (row.original.default ? t('common.yes') : t('common.no')),
      },
      {
        id: 'refs',
        accessorFn: (img) => img.refCount ?? 0,
        header: t('common.refs'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => row.original.refCount ?? '—',
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        meta: { headerClassName: 'text-right', className: 'text-right' },
        cell: ({ row }) =>
          !row.original.default ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => setDeleteTarget(row.original)}
            >
              <Trash2 size={14} />
            </Button>
          ) : null,
      },
    ],
    [t],
  )

  return (
    <div data-testid="engine-images-page">
      <PageHeader
        title={t('engineImages.title')}
        description={t('engineImages.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            <Button type="button" size="sm" onClick={() => setOpen(true)}>
              <Plus size={14} /> {t('common.deploy')}
            </Button>
          </>
        }
      />
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('engineImages.empty')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="engine-images-table"
          columns={columns}
          data={q.data ?? []}
          getRowId={(img) => img.id ?? img.name}
          tableId="engine-images"
          searchable
          enableExport
          exportName="highland-engine-images"
          enableSelection
          bulkActions={(sel) => (
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
              <Trash2 size={14} /> {t('common.delete')}
            </Button>
          )}
        />
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('engineImages.deploy')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setError(null)
                void createMut
                  .mutateAsync({ image })
                  .then(() => setOpen(false))
                  .catch((e: Error) => setError(e.message))
              }}
            >
              {t('common.deploy')}
            </Button>
          </>
        }
      >
        <Input value={image} onChange={(e) => setImage(e.target.value)} />
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('engineImages.delete')}
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
        title={t('engineImages.delete')}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          for (const img of bulkRows) {
            if (img.default) continue
            await delMut.mutateAsync(img)
          }
          setBulkDeleteOpen(false)
          setBulkRows([])
        }}
      />
    </div>
  )
}
