import { useMemo, useState } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import {
  useCreateRecurringJob,
  useDeleteRecurringJob,
  useRecurringJobs,
  useUpdateRecurringJob,
} from '@/api/hooks'
import type { RecurringJob } from '@/api/longhorn'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const TASKS = [
  'snapshot',
  'snapshot-force-create',
  'snapshot-cleanup',
  'snapshot-delete',
  'backup',
  'backup-force-create',
  'filesystem-trim',
  'system-backup',
] as const

const POLICY_OPTIONS = ['if-not-present', 'always', 'disabled'] as const

type FormState = {
  name: string
  task: string
  cron: string
  retain: string
  concurrency: string
  groups: string
  labels: string
  policy: string
}

function emptyForm(): FormState {
  return {
    name: '',
    task: 'snapshot',
    cron: '0 0 * * *',
    retain: '5',
    concurrency: '1',
    groups: 'default',
    labels: '',
    policy: 'if-not-present',
  }
}

function formFromJob(job: RecurringJob): FormState {
  const labels = job.labels ?? {}
  const params = job.parameters ?? {}
  return {
    name: job.name ?? '',
    task: job.task ?? 'snapshot',
    cron: job.cron ?? '',
    retain: String(job.retain ?? ''),
    concurrency: String(job.concurrency ?? ''),
    groups: (job.groups ?? []).join(', '),
    labels: Object.entries(labels)
      .map(([k, v]) => `${k}=${v}`)
      .join('\n'),
    policy: params['volume-backup-policy'] ?? 'if-not-present',
  }
}

function parseLabels(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const idx = trimmed.indexOf('=')
    if (idx <= 0) continue
    const key = trimmed.slice(0, idx).trim()
    const value = trimmed.slice(idx + 1).trim()
    if (key) out[key] = value
  }
  return out
}

/** Build the request body from the form, only including fields that are set. */
function buildBody(f: FormState): Record<string, unknown> {
  const body: Record<string, unknown> = {
    name: f.name.trim(),
    task: f.task,
  }
  if (f.cron.trim()) body.cron = f.cron.trim()
  if (f.retain.trim() !== '') body.retain = Number(f.retain) || 0
  const groups = f.groups
    .split(',')
    .map((g) => g.trim())
    .filter(Boolean)
  const labels = parseLabels(f.labels)

  if (f.task === 'system-backup') {
    // system-backup has no concurrency/groups/labels but carries a policy parameter
    body.parameters = { 'volume-backup-policy': f.policy }
  } else {
    if (f.concurrency.trim() !== '') body.concurrency = Number(f.concurrency) || 1
    body.groups = groups
    if (Object.keys(labels).length > 0) body.labels = labels
  }
  return body
}

export function RecurringJobsPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useRecurringJobs()
  const createMut = useCreateRecurringJob()
  const updateMut = useUpdateRecurringJob()
  const delMut = useDeleteRecurringJob()

  const [open, setOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<RecurringJob | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<RecurringJob | null>(null)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [bulkRows, setBulkRows] = useState<RecurringJob[]>([])

  const isEdit = Boolean(editTarget)
  const isSystemBackup = form.task === 'system-backup'

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function openCreate() {
    setError(null)
    setEditTarget(null)
    setForm(emptyForm())
    setOpen(true)
  }

  function openEdit(job: RecurringJob) {
    setError(null)
    setEditTarget(job)
    setForm(formFromJob(job))
    setOpen(true)
  }

  async function onSubmit() {
    setError(null)
    try {
      const body = buildBody(form)
      if (editTarget) {
        await updateMut.mutateAsync({ job: editTarget, body })
      } else {
        await createMut.mutateAsync(body)
      }
      setOpen(false)
      setEditTarget(null)
      setForm(emptyForm())
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  const pending = createMut.isPending || updateMut.isPending

  const columns = useMemo<ColumnDef<RecurringJob, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (job) => job.name ?? '',
        header: t('common.name'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.name,
      },
      {
        id: 'task',
        accessorFn: (job) => job.task ?? '',
        header: t('common.task'),
        cell: ({ row }) => <Badge>{row.original.task ?? '—'}</Badge>,
      },
      {
        id: 'cron',
        accessorFn: (job) => job.cron ?? '',
        header: t('common.cron'),
        meta: { className: 'font-mono text-xs' },
        cell: ({ row }) => row.original.cron,
      },
      {
        id: 'retain',
        accessorFn: (job) => job.retain ?? 0,
        header: t('common.retain'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => row.original.retain ?? '—',
      },
      {
        id: 'groups',
        accessorFn: (job) => (job.groups ?? []).join(', '),
        header: t('common.groups'),
        cell: ({ row }) => (row.original.groups ?? []).join(', ') || '—',
      },
      {
        id: 'actions',
        header: t('common.actions'),
        enableSorting: false,
        meta: { headerClassName: 'text-right', className: 'text-right' },
        cell: ({ row }) =>
          canMutate ? (
            <div className="flex justify-end gap-1">
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={t('common.edit')}
                onClick={() => openEdit(row.original)}
              >
                <Pencil size={14} aria-hidden />
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                onClick={() => setDeleteTarget(row.original)}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          ) : null,
      },
    ],
    [t, canMutate],
  )

  const data = q.data ?? []

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
            {canMutate ? (
              <Button type="button" size="sm" onClick={openCreate}>
                <Plus size={14} /> {t('common.create')}
              </Button>
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
        emptyTitle={t('recurringJobs.empty')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="recurring-jobs-table"
          columns={columns}
          data={data}
          getRowId={(job) => job.id ?? job.name}
          tableId="recurring-jobs"
          searchable
          enableExport
          exportName="highland-recurring-jobs"
          enableSelection
          bulkActions={(sel) =>
            canMutate ? (
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
            ) : null
          }
        />
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={(v) => {
          setOpen(v)
          if (!v) setEditTarget(null)
        }}
        title={isEdit ? t('recurringJobs.edit') : t('recurringJobs.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => void onSubmit()}
              disabled={!form.name.trim() || pending}
            >
              {isEdit ? t('common.save') : t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="rj-name">{t('common.name')}</Label>
            <Input
              id="rj-name"
              value={form.name}
              disabled={isEdit}
              onChange={(e) => set('name', e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="rj-task">{t('common.task')}</Label>
            <Select id="rj-task" value={form.task} onChange={(e) => set('task', e.target.value)}>
              {TASKS.map((task) => (
                <option key={task} value={task}>
                  {task}
                </option>
              ))}
            </Select>
          </div>
          <div className="space-y-1">
            <Label htmlFor="rj-cron">{t('common.cron')}</Label>
            <Input
              id="rj-cron"
              value={form.cron}
              placeholder="0 0 * * *"
              onChange={(e) => set('cron', e.target.value)}
            />
            <p className="text-xs text-[var(--color-muted-foreground)]">
              {t('recurringJobs.cronHint')}
            </p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label htmlFor="rj-retain">{t('common.retain')}</Label>
              <Input
                id="rj-retain"
                type="number"
                min={0}
                value={form.retain}
                onChange={(e) => set('retain', e.target.value)}
              />
            </div>
            {!isSystemBackup ? (
              <div className="space-y-1">
                <Label htmlFor="rj-concurrency">{t('recurringJobs.concurrency')}</Label>
                <Input
                  id="rj-concurrency"
                  type="number"
                  min={1}
                  value={form.concurrency}
                  onChange={(e) => set('concurrency', e.target.value)}
                />
              </div>
            ) : null}
          </div>
          {isSystemBackup ? (
            <div className="space-y-1">
              <Label htmlFor="rj-policy">{t('recurringJobs.parameterPolicy')}</Label>
              <Select
                id="rj-policy"
                value={form.policy}
                onChange={(e) => set('policy', e.target.value)}
              >
                {POLICY_OPTIONS.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </Select>
            </div>
          ) : (
            <>
              <div className="space-y-1">
                <Label htmlFor="rj-groups">{t('common.groups')}</Label>
                <Input
                  id="rj-groups"
                  value={form.groups}
                  placeholder="default"
                  onChange={(e) => set('groups', e.target.value)}
                />
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {t('recurringJobs.groupsHint')}
                </p>
              </div>
              <div className="space-y-1">
                <Label htmlFor="rj-labels">{t('common.labels')}</Label>
                <textarea
                  id="rj-labels"
                  value={form.labels}
                  rows={3}
                  placeholder="key=value"
                  onChange={(e) => set('labels', e.target.value)}
                  className="flex w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 py-2 font-mono text-xs shadow-[var(--shadow-sm)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                />
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {t('recurringJobs.labelsHint')}
                </p>
              </div>
            </>
          )}
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

      <ConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={(v) => !v && setBulkDeleteOpen(false)}
        title={t('recurringJobs.delete')}
        destructive
        confirmLabel={t('common.delete')}
        loading={delMut.isPending}
        onConfirm={async () => {
          for (const job of bulkRows) {
            await delMut.mutateAsync(job)
          }
          setBulkDeleteOpen(false)
          setBulkRows([])
        }}
      />
    </div>
  )
}
