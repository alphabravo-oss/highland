import { useMemo, useState } from 'react'
import { RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { useDeleteOrphan, useOrphans } from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import type { Orphan } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function OrphansPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useOrphans()
  const delMut = useDeleteOrphan()
  const [deleteTarget, setDeleteTarget] = useState<Orphan | null>(null)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedOrphans, setSelectedOrphans] = useState<Orphan[]>([])

  const columns = useMemo<ColumnDef<Orphan, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (o) => o.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'type',
        accessorFn: (o) => o.orphanType ?? '',
        header: t('common.type'),
        cell: ({ row }) => <Badge>{row.original.orphanType ?? '—'}</Badge>,
      },
      {
        id: 'node',
        accessorFn: (o) => o.nodeID ?? '',
        header: t('common.node'),
        cell: ({ row }) => row.original.nodeID ?? '—',
      },
      {
        id: 'parameters',
        accessorFn: (o) => (o.parameters ? JSON.stringify(o.parameters) : ''),
        header: t('common.parameters'),
        meta: { className: 'max-w-sm truncate font-mono text-xs' },
        cell: ({ row }) =>
          row.original.parameters ? JSON.stringify(row.original.parameters) : '—',
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        meta: { className: 'text-right' },
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

  const data = q.data ?? []

  return (
    <div data-testid="orphans-page">
      <PageHeader
        title={t('orphans.title')}
        description={t('orphans.description')}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
            <RefreshCw size={14} /> {t('common.refresh')}
          </Button>
        }
      />
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!data.length}
        emptyTitle={t('orphans.empty')}
        emptyDescription={t('orphans.emptyDescription')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          columns={columns}
          data={data}
          getRowId={(o) => o.id ?? o.name}
          tableId="orphans"
          searchable
          enableExport
          exportName="highland-orphans"
          enableSelection
          onSelectionChange={setSelectedOrphans}
          bulkActions={() =>
            canMutate ? (
              <Button
                type="button"
                size="sm"
                variant="destructive"
                className="h-7 gap-1 text-xs"
                onClick={() => setBulkDeleteOpen(true)}
              >
                <Trash2 size={14} aria-hidden /> {t('common.delete')}
              </Button>
            ) : null
          }
        />
      </QueryState>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('orphans.delete')}
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
        title={t('orphans.delete')}
        description={t('table.selectedCount', { count: selectedOrphans.length })}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          for (const orphan of selectedOrphans) {
            await delMut.mutateAsync(orphan)
          }
          setBulkDeleteOpen(false)
        }}
      />
    </div>
  )
}
