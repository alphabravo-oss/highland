import { useMemo, useState } from 'react'
import { Download, LifeBuoy, RefreshCw } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { useCreateSupportBundle, useSupportBundles } from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import { toHighlandPath } from '@/api/client'
import type { SupportBundle } from '@/api/longhorn'
import { DataTable } from '@/components/data/DataTable'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function SupportBundlePage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useSupportBundles()
  const createMut = useCreateSupportBundle()
  const [error, setError] = useState<string | null>(null)
  const [issueURL, setIssueURL] = useState('')
  const [description, setDescription] = useState(() => t('supportBundle.defaultDescription'))

  async function create() {
    setError(null)
    try {
      await createMut.mutateAsync({
        issueURL: issueURL || undefined,
        description,
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  const rows = useMemo(() => q.data ?? [], [q.data])

  const columns = useMemo<ColumnDef<SupportBundle, any>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (b) => b.name ?? b.id ?? '',
        header: t('common.name'),
        cell: ({ row }) => (
          <span className="font-medium">{row.original.name ?? row.original.id}</span>
        ),
      },
      {
        id: 'state',
        accessorFn: (b) => b.state ?? '',
        header: t('common.state'),
        cell: ({ row }) => <Badge tone={stateTone(row.original.state)}>{row.original.state ?? '—'}</Badge>,
      },
      {
        id: 'progress',
        accessorFn: (b) => b.progress ?? 0,
        header: t('common.progress'),
        meta: { className: 'tabular-nums' },
        cell: ({ row }) => (row.original.progress != null ? `${row.original.progress}%` : '—'),
      },
      {
        id: 'error',
        accessorFn: (b) => b.errorMessage ?? '',
        header: t('common.error'),
        meta: { className: 'max-w-xs truncate text-xs' },
        cell: ({ row }) => row.original.errorMessage ?? '—',
      },
      {
        id: 'download',
        enableSorting: false,
        header: t('common.download'),
        meta: { headerClassName: 'text-right', className: 'text-right' },
        cell: ({ row }) => {
          const b = row.original
          const download = b.links?.download || b.imageURL || b.actions?.download
          return download ? (
            <a
              href={toHighlandPath(download)}
              className="inline-flex items-center gap-1 text-sm text-[var(--color-primary)] hover:underline"
            >
              <Download size={14} aria-hidden /> {t('common.download')}
            </a>
          ) : (
            '—'
          )
        },
      },
    ],
    [t],
  )

  return (
    <div data-testid="support-bundle-page">
      <PageHeader
        title={t('supportBundle.title')}
        description={t('supportBundle.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            {canMutate ? (
              <Button
                type="button"
                size="sm"
                onClick={() => void create()}
                disabled={createMut.isPending}
                data-testid="create-support-bundle"
              >
                <LifeBuoy size={14} /> {t('common.generate')}
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

      <div className="mb-4 grid max-w-xl gap-2">
        <Input
          placeholder={t('supportBundle.issueUrl')}
          value={issueURL}
          onChange={(e) => setIssueURL(e.target.value)}
        />
        <Input
          placeholder={t('supportBundle.descriptionPlaceholder')}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!rows.length}
        emptyTitle={t('supportBundle.empty')}
        emptyDescription={t('supportBundle.emptyDescription')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="support-bundles-table"
          columns={columns}
          data={rows}
          getRowId={(b) => b.id ?? b.name ?? ''}
          tableId="support-bundles"
          searchable
          enableExport
          exportName="highland-support-bundles"
        />
      </QueryState>
    </div>
  )
}
