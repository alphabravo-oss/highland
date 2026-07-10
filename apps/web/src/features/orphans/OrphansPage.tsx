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
import { cn } from '@/lib/utils'

// Longhorn orphanType values that represent orphaned instances (engine/replica
// process instances) as opposed to on-disk replica data. Anything that is not
// one of these — including missing/unknown types — is treated as replica data.
const INSTANCE_ORPHAN_TYPES = new Set(['instance', 'engine-instance', 'replica-instance'])

function isInstanceOrphan(o: Orphan): boolean {
  return INSTANCE_ORPHAN_TYPES.has(o.orphanType ?? '')
}

type OrphanTab = 'replicaData' | 'instances'

export function OrphansPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useOrphans()
  const delMut = useDeleteOrphan()
  const [deleteTarget, setDeleteTarget] = useState<Orphan | null>(null)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedOrphans, setSelectedOrphans] = useState<Orphan[]>([])
  const [activeTab, setActiveTab] = useState<OrphanTab>('replicaData')

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

  const { replicaData, instances } = useMemo(() => {
    const replicaData: Orphan[] = []
    const instances: Orphan[] = []
    for (const o of data) {
      if (isInstanceOrphan(o)) instances.push(o)
      else replicaData.push(o)
    }
    return { replicaData, instances }
  }, [data])

  const tabs: { id: OrphanTab; label: string; count: number }[] = [
    { id: 'replicaData', label: t('orphans.tabs.replicaData'), count: replicaData.length },
    { id: 'instances', label: t('orphans.tabs.instances'), count: instances.length },
  ]

  const currentData = activeTab === 'instances' ? instances : replicaData

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
        <div
          role="tablist"
          aria-label={t('orphans.title')}
          className="mb-4 inline-flex gap-1 rounded-md border border-[var(--color-border)] bg-[var(--color-muted)] p-1"
        >
          {tabs.map((tab) => {
            const selected = activeTab === tab.id
            return (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={selected}
                onClick={() => {
                  setActiveTab(tab.id)
                  setSelectedOrphans([])
                }}
                className={cn(
                  'inline-flex items-center gap-2 rounded px-3 py-1.5 text-sm font-medium transition-colors',
                  selected
                    ? 'bg-[var(--color-card)] text-[var(--color-foreground)] shadow-[var(--shadow-sm)]'
                    : 'text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]',
                )}
              >
                {tab.label}
                <Badge tone={selected ? 'primary' : 'default'}>{tab.count}</Badge>
              </button>
            )
          })}
        </div>

        <DataTable
          key={activeTab}
          columns={columns}
          data={currentData}
          getRowId={(o) => o.id ?? o.name}
          tableId={`orphans-${activeTab}`}
          searchable
          enableExport
          exportName={`highland-orphans-${activeTab}`}
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
