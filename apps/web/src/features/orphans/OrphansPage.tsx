import { useState } from 'react'
import { RefreshCw, Trash2 } from 'lucide-react'
import { useDeleteOrphan, useOrphans } from '@/api/hooks'
import type { Orphan } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function OrphansPage() {
  const { t } = useAppTranslation()
  const q = useOrphans()
  const delMut = useDeleteOrphan()
  const [deleteTarget, setDeleteTarget] = useState<Orphan | null>(null)

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
        isEmpty={!q.data?.length}
        emptyTitle={t('orphans.empty')}
        emptyDescription={t('orphans.emptyDescription')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.type')}</TH>
              <TH>{t('common.node')}</TH>
              <TH>{t('common.parameters')}</TH>
              <TH />
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((o) => (
              <TR key={o.id ?? o.name}>
                <TD className="font-medium">{o.name}</TD>
                <TD>
                  <Badge>{o.orphanType ?? '—'}</Badge>
                </TD>
                <TD>{o.nodeID ?? '—'}</TD>
                <TD className="max-w-sm truncate font-mono text-xs">
                  {o.parameters ? JSON.stringify(o.parameters) : '—'}
                </TD>
                <TD className="text-right">
                  <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(o)}>
                    <Trash2 size={14} />
                  </Button>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
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
    </div>
  )
}
