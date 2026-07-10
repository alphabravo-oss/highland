import { useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  useCreateRecurringJob,
  useDeleteRecurringJob,
  useRecurringJobs,
} from '@/api/hooks'
import type { RecurringJob } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function RecurringJobsPage() {
  const { t } = useAppTranslation()
  const q = useRecurringJobs()
  const createMut = useCreateRecurringJob()
  const delMut = useDeleteRecurringJob()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [task, setTask] = useState('snapshot')
  const [cron, setCron] = useState('0 */4 * * *')
  const [retain, setRetain] = useState('5')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<RecurringJob | null>(null)

  async function onCreate() {
    setError(null)
    try {
      await createMut.mutateAsync({
        name,
        task,
        cron,
        retain: Number(retain) || 5,
        concurrency: 1,
        groups: ['default'],
        labels: {},
      })
      setOpen(false)
      setName('')
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  return (
    <div data-testid="recurring-jobs-page">
      <PageHeader
        title={t('recurringJobs.title')}
        description={t('recurringJobs.description')}
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
        emptyTitle={t('recurringJobs.empty')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.task')}</TH>
              <TH>{t('common.cron')}</TH>
              <TH>{t('common.retain')}</TH>
              <TH>{t('common.groups')}</TH>
              <TH className="text-right">{t('common.actions')}</TH>
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((job) => (
              <TR key={job.id ?? job.name}>
                <TD className="font-medium">{job.name}</TD>
                <TD>
                  <Badge>{job.task ?? '—'}</Badge>
                </TD>
                <TD className="font-mono text-xs">{job.cron}</TD>
                <TD className="tabular-nums">{job.retain ?? '—'}</TD>
                <TD>{(job.groups ?? []).join(', ') || '—'}</TD>
                <TD className="text-right">
                  <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(job)}>
                    <Trash2 size={14} />
                  </Button>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('recurringJobs.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void onCreate()} disabled={!name || createMut.isPending}>
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
            <span className="font-medium">{t('recurringJobs.taskHint')}</span>
            <Input value={task} onChange={(e) => setTask(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('common.cron')}</span>
            <Input value={cron} onChange={(e) => setCron(e.target.value)} />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="font-medium">{t('common.retain')}</span>
            <Input value={retain} onChange={(e) => setRetain(e.target.value)} />
          </label>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('recurringJobs.delete')}
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
