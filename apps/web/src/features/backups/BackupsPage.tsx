import { useState } from 'react'
import { RefreshCw, Trash2 } from 'lucide-react'
import { useBackupVolumes, useCreateVolume, useDeleteBackupVolume } from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { backupVolumesApi, formatBytes, hasAction, lhPut, type BackupVolume } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function BackupsPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useBackupVolumes()
  const delMut = useDeleteBackupVolume()
  const createVol = useCreateVolume()
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BackupVolume | null>(null)
  const [restoreTarget, setRestoreTarget] = useState<BackupVolume | null>(null)
  const [restoreName, setRestoreName] = useState('')
  const [standby, setStandby] = useState(false)
  const [backupList, setBackupList] = useState<Array<Record<string, unknown>>>([])
  const [backupListFor, setBackupListFor] = useState<string | null>(null)
  const [selected, setSelected] = useState<Record<string, boolean>>({})

  async function listBackups(bv: BackupVolume) {
    setError(null)
    try {
      if (!hasAction(bv, 'backupList')) {
        setError('backupList action not available')
        return
      }
      const res = (await backupVolumesApi.action(bv, 'backupList', {})) as {
        data?: Array<Record<string, unknown>>
      }
      const data = Array.isArray(res) ? res : (res?.data ?? [])
      setBackupList(data as Array<Record<string, unknown>>)
      setBackupListFor(bv.name)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'List backups failed')
    }
  }

  async function syncOne(bv: BackupVolume) {
    setError(null)
    try {
      const action = hasAction(bv, 'backupVolumeSync')
        ? 'backupVolumeSync'
        : hasAction(bv, 'sync')
          ? 'sync'
          : null
      if (!action) {
        setError('sync action not available')
        return
      }
      await backupVolumesApi.action(bv, action, {})
      await q.refetch()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Sync failed')
    }
  }

  async function syncAll() {
    setError(null)
    try {
      await lhPut('/backupvolumes', {})
      await q.refetch()
    } catch (e) {
      // try collection action
      try {
        for (const bv of q.data ?? []) {
          if (hasAction(bv, 'backupVolumeSync')) {
            await backupVolumesApi.action(bv, 'backupVolumeSync', {})
          }
        }
        await q.refetch()
      } catch (e2) {
        setError(e2 instanceof Error ? e2.message : 'Sync all failed')
      }
    }
  }

  async function restore() {
    if (!restoreTarget || !restoreName) return
    setError(null)
    try {
      const last = restoreTarget.lastBackupName
      await createVol.mutateAsync({
        name: restoreName,
        size: restoreTarget.size,
        numberOfReplicas: 3,
        frontend: standby ? '' : 'blockdev',
        standby,
        fromBackup: last ? `backup://${restoreTarget.name}/${last}` : undefined,
      })
      setRestoreTarget(null)
      setRestoreName('')
      setStandby(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Restore failed')
    }
  }

  async function deleteBackupEntry(bv: BackupVolume, backupName: string) {
    setError(null)
    try {
      if (hasAction(bv, 'backupDelete')) {
        await backupVolumesApi.action(bv, 'backupDelete', { name: backupName })
      } else {
        setError('backupDelete not available')
        return
      }
      await listBackups(bv)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete backup failed')
    }
  }

  async function bulkRestore() {
    const targets = (q.data ?? []).filter((b) => selected[b.name])
    for (const bv of targets) {
      setRestoreTarget(bv)
      setRestoreName(`${bv.name}-restore`)
      // sequential dialog-free restore
      try {
        const last = bv.lastBackupName
        await createVol.mutateAsync({
          name: `${bv.name}-restore-${Date.now()}`,
          size: bv.size,
          numberOfReplicas: 3,
          frontend: 'blockdev',
          fromBackup: last ? `backup://${bv.name}/${last}` : undefined,
        })
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Bulk restore failed')
        break
      }
    }
    setSelected({})
  }

  return (
    <div data-testid="backups-page">
      <PageHeader
        title={t('backups.title')}
        description={t('backups.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            {canMutate ? (
              <>
                <Button type="button" size="sm" variant="outline" onClick={() => void syncAll()}>
                  {t('backups.syncAll')}
                </Button>
                {Object.values(selected).some(Boolean) ? (
                  <Button type="button" size="sm" onClick={() => void bulkRestore()}>
                    {t('backups.bulkRestore')}
                  </Button>
                ) : null}
              </>
            ) : null}
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
        emptyTitle={t('backups.emptyVolumes')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH className="w-8" />
              <TH>{t('backups.volume')}</TH>
              <TH>{t('common.size')}</TH>
              <TH>{t('backups.lastBackup')}</TH>
              <TH>{t('backups.lastBackupAt')}</TH>
              <TH>{t('backups.target')}</TH>
              <TH>{t('common.labels')}</TH>
              <TH className="text-right">{t('common.actions')}</TH>
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((bv) => (
              <TR key={bv.id ?? bv.name}>
                <TD>
                  <input
                    type="checkbox"
                    checked={Boolean(selected[bv.name])}
                    onChange={(e) => setSelected((s) => ({ ...s, [bv.name]: e.target.checked }))}
                  />
                </TD>
                <TD className="font-medium">{bv.name}</TD>
                <TD className="tabular-nums">{formatBytes(bv.size)}</TD>
                <TD>{bv.lastBackupName ?? '—'}</TD>
                <TD className="whitespace-nowrap text-xs">{bv.lastBackupAt ?? '—'}</TD>
                <TD>{bv.backupTargetName ?? '—'}</TD>
                <TD className="max-w-[8rem] truncate text-xs">
                  {bv.messages ? Object.keys(bv.messages).join(',') : '—'}
                </TD>
                <TD>
                  <div className="flex flex-wrap justify-end gap-1">
                    <Button type="button" size="sm" variant="outline" onClick={() => void listBackups(bv)}>
                      {t('common.list')}
                    </Button>
                    {canMutate ? (
                      <>
                        <Button type="button" size="sm" variant="outline" onClick={() => void syncOne(bv)}>
                          {t('common.sync')}
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setRestoreTarget(bv)
                            setRestoreName(`${bv.name}-restore`)
                            setStandby(false)
                          }}
                        >
                          {t('backups.restore')}
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setRestoreTarget(bv)
                            setRestoreName(`${bv.name}-dr`)
                            setStandby(true)
                          }}
                        >
                          {t('backups.drStandby')}
                        </Button>
                        <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(bv)}>
                          <Trash2 size={14} />
                        </Button>
                      </>
                    ) : null}
                  </div>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      </QueryState>

      {backupListFor ? (
        <div className="mt-4">
          <h3 className="mb-2 text-sm font-semibold">
            {t('backups.backupsFor', { name: backupListFor })}{' '}
            <Badge>{backupList.length}</Badge>
          </h3>
          <Table>
            <THead>
              <TR>
                <TH>{t('common.name')}</TH>
                <TH>{t('common.created')}</TH>
                <TH>{t('common.size')}</TH>
                <TH>{t('common.labels')}</TH>
                <TH />
              </TR>
            </THead>
            <TBody>
              {backupList.map((b, i) => (
                <TR key={String(b.name ?? i)}>
                  <TD>{String(b.name ?? b.id ?? '—')}</TD>
                  <TD className="text-xs">{String(b.created ?? b.snapshotCreated ?? '—')}</TD>
                  <TD className="tabular-nums">{formatBytes(b.size as string | number | undefined)}</TD>
                  <TD className="max-w-xs truncate text-xs">
                    {b.labels ? JSON.stringify(b.labels) : '—'}
                  </TD>
                  <TD>
                    {canMutate ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          const bv = (q.data ?? []).find((x) => x.name === backupListFor)
                          if (bv) void deleteBackupEntry(bv, String(b.name ?? ''))
                        }}
                      >
                        {t('common.delete')}
                      </Button>
                    ) : null}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </div>
      ) : null}

      <Dialog
        open={Boolean(restoreTarget)}
        onOpenChange={(v) => !v && setRestoreTarget(null)}
        title={standby ? t('backups.createDrStandby') : t('backups.restoreTitle')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setRestoreTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void restore()} disabled={createVol.isPending}>
              {standby ? t('backups.createStandby') : t('backups.restore')}
            </Button>
          </>
        }
      >
        <label className="block space-y-1 text-sm">
          <span className="font-medium">{t('backups.newVolumeName')}</span>
          <Input value={restoreName} onChange={(e) => setRestoreName(e.target.value)} />
        </label>
        {standby ? (
          <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">
            {t('backups.standbyHint')}
          </p>
        ) : null}
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('backups.deleteBackupVolume')}
        confirmText={deleteTarget?.name}
        destructive
        confirmLabel={t('backups.deleteAllBackups')}
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
