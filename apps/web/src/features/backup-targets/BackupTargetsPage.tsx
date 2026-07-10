import { useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  useBackupTargets,
  useCreateBackupTarget,
  useDeleteBackupTarget,
} from '@/api/hooks'
import { hasAction, type BackupTarget } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { backupTargetsApi } from '@/api/longhorn'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function BackupTargetsPage() {
  const { t } = useAppTranslation()
  const q = useBackupTargets()
  const createMut = useCreateBackupTarget()
  const delMut = useDeleteBackupTarget()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('default')
  const [url, setUrl] = useState('s3://bucket@us-east-1/')
  const [secret, setSecret] = useState('')
  const [poll, setPoll] = useState('300')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BackupTarget | null>(null)

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
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
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
        setError('No sync/update action on target')
      }
      await q.refetch()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Sync failed')
    }
  }

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
            <Button type="button" size="sm" onClick={() => setOpen(true)}>
              <Plus size={14} /> {t('common.create')}
            </Button>
          </>
        }
      />
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
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.url')}</TH>
              <TH>{t('common.available')}</TH>
              <TH>{t('common.poll')}</TH>
              <TH>{t('common.message')}</TH>
              <TH className="text-right">{t('common.actions')}</TH>
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((bt) => (
              <TR key={bt.id ?? bt.name}>
                <TD className="font-medium">{bt.name}</TD>
                <TD className="max-w-xs truncate font-mono text-xs">{bt.backupTargetURL}</TD>
                <TD>
                  <Badge tone={stateTone(bt.available ? 'available' : 'faulted')}>
                    {bt.available ? t('common.yes') : t('common.no')}
                  </Badge>
                </TD>
                <TD>{bt.pollInterval ?? '—'}</TD>
                <TD className="max-w-xs truncate text-xs">{bt.message ?? '—'}</TD>
                <TD>
                  <div className="flex justify-end gap-1">
                    <Button type="button" size="sm" variant="outline" onClick={() => void syncTarget(bt)}>
                      {t('common.sync')}
                    </Button>
                    <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(bt)}>
                      <Trash2 size={14} />
                    </Button>
                  </div>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
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
          await delMut.mutateAsync(deleteTarget)
          setDeleteTarget(null)
        }}
      />
    </div>
  )
}
