import { useMemo, useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useCreateEngineImage,
  useDeleteEngineImage,
  useEngineImages,
} from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
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
import { TableSkeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function EngineImagesPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const toast = useToast()
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
          canMutate && !row.original.default ? (
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
            {canMutate ? (
              <Button type="button" size="sm" onClick={() => setOpen(true)}>
                <Plus size={14} /> {t('common.deploy')}
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
        emptyTitle={t('engineImages.empty')}
        emptyAction={
          canMutate ? (
            <Button type="button" size="sm" onClick={() => setOpen(true)}>
              <Plus size={14} /> {t('engineImages.deploy')}
            </Button>
          ) : undefined
        }
        skeleton={<TableSkeleton rows={6} cols={5} />}
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
          bulkActions={
            canMutate
              ? (sel) => (
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
                )
              : undefined
          }
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
                  .then(() => {
                    setOpen(false)
                    toast.success(t('engineImages.deployedToast', { image }))
                  })
                  .catch((e: Error) => {
                    setError(e.message)
                    toast.error(t('engineImages.deploy'), e.message)
                  })
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
          const name = deleteTarget.name
          try {
            await delMut.mutateAsync(deleteTarget)
            toast.success(t('engineImages.deletedToast', { name }))
          } catch (e) {
            toast.error(t('engineImages.delete'), e instanceof Error ? e.message : undefined)
          }
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
          let ok = 0
          const failed: string[] = []
          for (const img of bulkRows) {
            if (img.default) continue
            try {
              await delMut.mutateAsync(img)
              ok++
            } catch {
              failed.push(img.name)
            }
          }
          setBulkDeleteOpen(false)
          setBulkRows([])
          const label = t('common.delete')
          if (failed.length) {
            toast.error(
              t('table.bulkResult', { action: label }),
              [
                t('table.bulkOk', { count: ok }),
                t('table.bulkFailed', { count: failed.length, names: failed.slice(0, 3).join(', ') }),
              ].join(' · '),
            )
          } else {
            toast.success(t('table.bulkResult', { action: label }), t('table.bulkOk', { count: ok }))
          }
        }}
      />
    </div>
  )
}
