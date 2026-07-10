import { RefreshCw } from 'lucide-react'
import { useInstanceManagers } from '@/api/hooks'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function InstanceManagersPage() {
  const { t } = useAppTranslation()
  const q = useInstanceManagers()
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
        isEmpty={!q.data?.length}
        emptyTitle={t('instanceManagers.empty')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.node')}</TH>
              <TH>{t('common.type')}</TH>
              <TH>{t('common.state')}</TH>
              <TH>{t('common.image')}</TH>
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((im) => (
              <TR key={im.id ?? im.name}>
                <TD className="font-medium">{im.name}</TD>
                <TD>{im.nodeID ?? '—'}</TD>
                <TD>{im.instanceManagerType ?? '—'}</TD>
                <TD>
                  <Badge tone={stateTone(im.currentState)}>{im.currentState ?? '—'}</Badge>
                </TD>
                <TD className="max-w-xs truncate font-mono text-xs">{im.image ?? '—'}</TD>
              </TR>
            ))}
          </TBody>
        </Table>
      </QueryState>
    </div>
  )
}
