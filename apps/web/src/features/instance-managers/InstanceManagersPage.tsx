import { useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { useInstanceManagers } from '@/api/hooks'
import type { InstanceManager } from '@/api/longhorn'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type InstanceInfo = { name: string; state?: string }

/**
 * Longhorn `instancemanager` objects expose the instances they hold as a map
 * of instance-name -> instance info. Depending on the Longhorn version this is
 * either a single `instances` map or split into `instanceEngines` /
 * `instanceReplicas` maps. This normalises all of those into a flat list.
 */
function getManagedInstances(im: InstanceManager): InstanceInfo[] {
  const source = im as {
    instances?: Record<string, unknown>
    instanceEngines?: Record<string, unknown>
    instanceReplicas?: Record<string, unknown>
  }
  const maps = source.instances
    ? [source.instances]
    : [source.instanceEngines, source.instanceReplicas]
  const out: InstanceInfo[] = []
  for (const map of maps) {
    if (!map) continue
    for (const [name, info] of Object.entries(map)) {
      out.push({ name, state: readState(info) })
    }
  }
  return out.sort((a, b) => a.name.localeCompare(b.name))
}

function readState(info: unknown): string | undefined {
  if (!info || typeof info !== 'object') return undefined
  const obj = info as { state?: unknown; status?: { state?: unknown }; currentState?: unknown }
  const state = obj.status?.state ?? obj.state ?? obj.currentState
  return typeof state === 'string' ? state : undefined
}

export function InstanceManagersPage() {
  const { t } = useAppTranslation()
  const q = useInstanceManagers()
  const [selected, setSelected] = useState<InstanceManager | null>(null)

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
        id: 'instances',
        accessorFn: (im) => getManagedInstances(im).length,
        header: t('instanceManagers.instances'),
        cell: ({ row }) => getManagedInstances(row.original).length,
      },
      {
        id: 'image',
        accessorFn: (im) => im.image ?? '',
        header: t('common.image'),
        meta: { className: 'max-w-xs truncate font-mono text-xs' },
        cell: ({ row }) => row.original.image ?? '—',
      },
      {
        id: 'actions',
        header: t('common.actions'),
        enableSorting: false,
        cell: ({ row }) => (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setSelected(row.original)}
          >
            {t('instanceManagers.viewInstances')}
          </Button>
        ),
      },
    ],
    [t],
  )

  const data = q.data ?? []
  const selectedInstances = selected ? getManagedInstances(selected) : []

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
          tableId="instance-managers"
          searchable
          enableExport
          exportName="highland-instance-managers"
        />
      </QueryState>

      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('instanceManagers.instancesFor', { name: selected?.name ?? '' })}
        description={t('instanceManagers.instancesCount', { count: selectedInstances.length })}
      >
        {selectedInstances.length ? (
          <Table>
            <THead>
              <TR>
                <TH>{t('instanceManagers.instanceName')}</TH>
                <TH>{t('common.state')}</TH>
              </TR>
            </THead>
            <TBody>
              {selectedInstances.map((inst) => (
                <TR key={inst.name}>
                  <TD className="font-mono text-xs">{inst.name}</TD>
                  <TD>
                    {inst.state ? (
                      <Badge tone={stateTone(inst.state)}>{inst.state}</Badge>
                    ) : (
                      '—'
                    )}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        ) : (
          <p className="text-sm text-[var(--color-muted-foreground)]">
            {t('instanceManagers.noInstances')}
          </p>
        )}
      </Dialog>
    </div>
  )
}
