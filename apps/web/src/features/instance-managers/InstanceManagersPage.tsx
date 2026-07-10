import { useMemo } from 'react'
import { RefreshCw } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { useInstanceManagers } from '@/api/hooks'
import type { InstanceManager } from '@/api/longhorn'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function InstanceManagersPage() {
  const { t } = useAppTranslation()
  const q = useInstanceManagers()

  const columns = useMemo<ColumnDef<InstanceManager, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (im) => im.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'node',
        accessorFn: (im) => im.nodeID ?? '',
        header: t('common.node'),
        cell: ({ row }) => row.original.nodeID ?? '—',
      },
      {
        id: 'type',
        accessorFn: (im) => im.instanceManagerType ?? '',
        header: t('common.type'),
        cell: ({ row }) => row.original.instanceManagerType ?? '—',
      },
      {
        id: 'state',
        accessorFn: (im) => im.currentState ?? '',
        header: t('common.state'),
        cell: ({ row }) => (
          <Badge tone={stateTone(row.original.currentState)}>{row.original.currentState ?? '—'}</Badge>
        ),
      },
      {
        id: 'image',
        accessorFn: (im) => im.image ?? '',
        header: t('common.image'),
        meta: { className: 'max-w-xs truncate font-mono text-xs' },
        cell: ({ row }) => row.original.image ?? '—',
      },
    ],
    [t],
  )

  const data = q.data ?? []

  return (
    <div data-testid="instance-managers-page">
      <PageHeader
        title={t('instanceManagers.title')}
        description={t('instanceManagers.description')}
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
        emptyTitle={t('instanceManagers.empty')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          columns={columns}
          data={data}
          getRowId={(im) => im.id ?? im.name}
        />
      </QueryState>
    </div>
  )
}
