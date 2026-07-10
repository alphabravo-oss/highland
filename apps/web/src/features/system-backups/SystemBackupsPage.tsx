import { useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useCreateSystemBackup,
  useDeleteSystemBackup,
  useSystemBackups,
  useSystemRestores,
} from '@/api/hooks'
import { systemRestoresApi, type SystemBackup } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function SystemBackupsPage() {
  const { t } = useAppTranslation()
  const backups = useSystemBackups()
  const restores = useSystemRestores()
  const createMut = useCreateSystemBackup()
  const delMut = useDeleteSystemBackup()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<SystemBackup | null>(null)

  const backupColumns: ColumnDef<SystemBackup, any>[] = [
    {
      id: 'name',
      accessorFn: (b) => b.name ?? '',
      header: t('common.name'),
      meta: { className: 'font-medium' },
      cell: ({ row }) => row.original.name,
    },
    {
      id: 'version',
      accessorFn: (b) => b.version ?? '',
      header: t('common.version'),
      cell: ({ row }) => row.original.version ?? '—',
    },
    {
      id: 'state',
      accessorFn: (b) => b.state ?? '',
      header: t('common.state'),
      cell: ({ row }) => <Badge tone={stateTone(row.original.state)}>{row.original.state ?? '—'}</Badge>,
    },
    {
      id: 'created',
      accessorFn: (b) => b.created ?? '',
      header: t('common.created'),
      meta: { className: 'text-xs' },
      cell: ({ row }) => row.original.created ?? '—',
    },
    {
      id: 'actions',
      header: () => null,
      enableSorting: false,
      meta: { headerClassName: 'text-right' },
      cell: ({ row }) => {
        const b = row.original
        return (
          <div className="flex justify-end gap-1">
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                setError(null)
                void systemRestoresApi
                  .create({
                    name: `restore-${b.name}-${Date.now()}`,
                    systemBackup: b.name,
                  })
                  .then(() => restores.refetch())
                  .catch((e: Error) => setError(e.message))
              }}
            >
              {t('common.restore')}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(b)}>
              <Trash2 size={14} />
            </Button>
          </div>
        )
      },
    },
  ]

  return (
    <div data-testid="system-backups-page">
      <PageHeader
        title={t('systemBackups.title')}
        description={t('systemBackups.description')}
        actions={
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                void backups.refetch()
                void restores.refetch()
              }}
            >
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            <Button type="button" size="sm" onClick={() => setOpen(true)}>
              <Plus size={14} /> {t('common.create')}
            </Button>
          </>
        }
      />
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}

      <div className="space-y-6">
        <QueryState
          isLoading={backups.isLoading}
          error={backups.error as Error | null}
          isEmpty={!backups.data?.length}
          emptyTitle={t('systemBackups.empty')}
          onRetry={() => void backups.refetch()}
        >
          <DataTable
            data-testid="system-backups-table"
            columns={backupColumns}
            data={backups.data ?? []}
            getRowId={(b) => b.id ?? b.name}
          />
        </QueryState>

        <Card>
          <CardHeader>
            <CardTitle>{t('systemBackups.restores')}</CardTitle>
          </CardHeader>
          <CardContent>
            <QueryState
              isLoading={restores.isLoading}
              error={restores.error as Error | null}
              isEmpty={!restores.data?.length}
              emptyTitle={t('systemBackups.noRestores')}
            >
              <Table>
                <THead>
                  <TR>
                    <TH>{t('common.name')}</TH>
                    <TH>{t('common.source')}</TH>
                    <TH>{t('common.state')}</TH>
                    <TH>{t('common.error')}</TH>
                  </TR>
                </THead>
                <TBody>
                  {(restores.data ?? []).map((r) => (
                    <TR key={r.id ?? r.name}>
                      <TD>{r.name}</TD>
                      <TD>{r.sourceSystemBackup ?? '—'}</TD>
                      <TD>
                        <Badge tone={stateTone(r.state)}>{r.state ?? '—'}</Badge>
                      </TD>
                      <TD className="max-w-xs truncate text-xs">{r.error ?? '—'}</TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            </QueryState>
          </CardContent>
        </Card>
      </div>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('systemBackups.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              disabled={!name}
              onClick={() => {
                setError(null)
                void createMut
                  .mutateAsync({ name })
                  .then(() => setOpen(false))
                  .catch((e: Error) => setError(e.message))
              }}
            >
              {t('common.create')}
            </Button>
          </>
        }
      >
        <Input placeholder={t('common.name')} value={name} onChange={(e) => setName(e.target.value)} />
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('systemBackups.delete')}
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
    </div>
  )
}
