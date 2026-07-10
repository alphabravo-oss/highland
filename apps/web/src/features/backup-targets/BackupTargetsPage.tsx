import { useMemo, useState } from 'react'
import { Plus, RefreshCw, Sparkles, Trash2 } from 'lucide-react'
import type { ColumnDef, RowSelectionState } from '@tanstack/react-table'
import {
  useBackupTargets,
  useCreateBackupTarget,
  useDeleteBackupTarget,
} from '@/api/hooks'
import { hasAction, type BackupTarget } from '@/api/longhorn'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { BackupSetupWizard } from './BackupSetupWizard'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { TableSkeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { backupTargetsApi } from '@/api/longhorn'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function BackupTargetsPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const toast = useToast()
  const q = useBackupTargets()
  const createMut = useCreateBackupTarget()
  const delMut = useDeleteBackupTarget()
  const [open, setOpen] = useState(false)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [name, setName] = useState('default')
  const [url, setUrl] = useState('s3://bucket@us-east-1/')
  const [secret, setSecret] = useState('')
  const [poll, setPoll] = useState('300')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BackupTarget | null>(null)
  const [selected, setSelected] = useState<RowSelectionState>({})
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)

  async function onCreate() {
    setError(null)
    try {
      await createMut.mutateAsync({
        name,
        backupTargetURL: url,
        credentialSecret: secret || undefined,
        pollInterval: `${poll}s`,
      })
      setOpen(false)
      toast.success(t('backupTargets.createdToast', { name }))
    } catch (e) {
      const msg = e instanceof Error ? e.message : t('admin.createFailed')
      setError(msg)
      toast.error(t('admin.createFailed'), msg)
    }
  }

  async function syncTarget(bt: BackupTarget) {
    setError(null)
    try {
      if (hasAction(bt, 'backupTargetSync')) {
        await backupTargetsApi.action(bt, 'backupTargetSync', {})
      } else if (hasAction(bt, 'backupTargetUpdate')) {
        await backupTargetsApi.action(bt, 'backupTargetUpdate', {
          backupTargetURL: bt.backupTargetURL,
          credentialSecret: bt.credentialSecret,
          pollInterval: bt.pollInterval,
        })
      } else {
        setError(t('volumeActions.actionFailed'))
        toast.error(t('volumeActions.actionFailed'))
        return
      }
      await q.refetch()
      toast.success(t('backupTargets.syncedToast', { name: bt.name }))
    } catch (e) {
      const msg = e instanceof Error ? e.message : t('volumeActions.actionFailed')
      setError(msg)
      toast.error(t('volumeActions.actionFailed'), msg)
    }
  }

  const columns = useMemo<ColumnDef<BackupTarget, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (bt) => bt.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'url',
        accessorFn: (bt) => bt.backupTargetURL ?? '',
        header: t('common.url'),
        meta: { className: 'max-w-xs truncate font-mono text-xs' },
        cell: ({ row }) => row.original.backupTargetURL,
      },
      {
        id: 'available',
        accessorFn: (bt) => (bt.available ? 1 : 0),
        header: t('common.available'),
        cell: ({ row }) => (
          <Badge tone={stateTone(row.original.available ? 'available' : 'faulted')}>
            {row.original.available ? t('common.yes') : t('common.no')}
          </Badge>
        ),
      },
      {
        id: 'poll',
        accessorFn: (bt) => bt.pollInterval ?? '',
        header: t('common.poll'),
        cell: ({ row }) => row.original.pollInterval ?? '—',
      },
      {
        id: 'message',
        accessorFn: (bt) => bt.message ?? '',
        header: t('common.message'),
        meta: { className: 'max-w-xs truncate text-xs' },
        cell: ({ row }) => row.original.message ?? '—',
      },
      {
        id: 'actions',
        header: t('common.actions'),
        enableSorting: false,
        meta: { headerClassName: 'text-right' },
        cell: ({ row }) => {
          const bt = row.original
          if (!canMutate) return null
          return (
            <div className="flex justify-end gap-1">
              <Button type="button" size="sm" variant="outline" onClick={() => void syncTarget(bt)}>
                {t('common.sync')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                onClick={() => setDeleteTarget(bt)}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          )
        },
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, canMutate],
  )

  const data = q.data ?? []

  return (
    <div data-testid="backup-targets-page">
      <PageHeader
        title={t('backupTargets.title')}
        description={t('backupTargets.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            {canMutate ? (
              <>
                <Button type="button" size="sm" onClick={() => setWizardOpen(true)}>
                  <Sparkles size={14} /> {t('backupWizard.setUp')}
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={() => setOpen(true)}>
                  <Plus size={14} /> {t('backupTargets.manual')}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      <BackupSetupWizard open={wizardOpen} onOpenChange={setWizardOpen} onDone={() => void q.refetch()} />
      {error ? (
        <Alert tone="danger" className="mb-3">
          {error}
        </Alert>
      ) : null}

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('backupTargets.empty')}
        emptyDescription={t('backupTargets.emptyDescription')}
        emptyAction={
          canMutate ? (
            <Button type="button" size="sm" onClick={() => setWizardOpen(true)}>
              <Sparkles size={14} /> {t('backupWizard.setUp')}
            </Button>
          ) : undefined
        }
        skeleton={<TableSkeleton rows={8} cols={6} />}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="backup-targets-table"
          columns={columns}
          data={data}
          getRowId={(bt) => bt.id ?? bt.name}
          tableId="backup-targets"
          searchable
          enableExport
          exportName="highland-backup-targets"
          enableSelection
          rowSelection={selected}
          onRowSelectionChange={setSelected}
          bulkActions={() =>
            canMutate ? (
              <Button
                type="button"
                size="sm"
                variant="destructive"
                className="h-7 text-xs"
                onClick={() => setBulkDeleteOpen(true)}
              >
                {t('common.delete')}
              </Button>
            ) : null
          }
        />
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('backupTargets.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void onCreate()} disabled={createMut.isPending}>
              {t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('common.name')}</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('common.url')}</span>
            <Input value={url} onChange={(e) => setUrl(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('backupTargets.credentialSecret')}</span>
            <Input value={secret} onChange={(e) => setSecret(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('backupTargets.pollInterval')}</span>
            <Input value={poll} onChange={(e) => setPoll(e.target.value)} />
          </label>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('backupTargets.delete')}
        confirmText={deleteTarget?.name}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          if (!deleteTarget) return
          const name = deleteTarget.name
          try {
            await delMut.mutateAsync(deleteTarget)
            toast.success(t('backupTargets.deletedToast', { name }))
          } catch (e) {
            toast.error(t('backupTargets.delete'), e instanceof Error ? e.message : undefined)
          }
          setDeleteTarget(null)
        }}
      />

      <ConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={(v) => !v && setBulkDeleteOpen(false)}
        title={t('backupTargets.delete')}
        description={t('table.selectedCount', {
          count: Object.values(selected).filter(Boolean).length,
        })}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          const targets = data.filter((bt) => selected[bt.id ?? bt.name])
          let ok = 0
          const failed: string[] = []
          for (const item of targets) {
            try {
              await delMut.mutateAsync(item)
              ok++
            } catch {
              failed.push(item.name)
            }
          }
          setSelected({})
          setBulkDeleteOpen(false)
          if (failed.length) {
            toast.error(
              t('backupTargets.delete'),
              t('table.bulkFailed', { count: failed.length, names: failed.slice(0, 3).join(', ') }),
            )
          } else if (ok) {
            toast.success(t('backupTargets.bulkDeletedToast', { count: ok }))
          }
        }}
      />
    </div>
  )
}
