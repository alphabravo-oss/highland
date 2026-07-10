import { useState } from 'react'
import { Download, LifeBuoy, RefreshCw } from 'lucide-react'
import { useCreateSupportBundle, useSupportBundles } from '@/api/hooks'
import { toHighlandPath } from '@/api/client'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function SupportBundlePage() {
  const { t } = useAppTranslation()
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
            <Button
              type="button"
              size="sm"
              onClick={() => void create()}
              disabled={createMut.isPending}
              data-testid="create-support-bundle"
            >
              <LifeBuoy size={14} /> {t('common.generate')}
            </Button>
          </>
        }
      />

      {error ? (
        <Alert tone="danger" className="mb-3">
          {error}
        </Alert>
      ) : null}

      <div className="mb-4 grid max-w-xl gap-2">
        <input
          className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
          placeholder={t('supportBundle.issueUrl')}
          value={issueURL}
          onChange={(e) => setIssueURL(e.target.value)}
        />
        <input
          className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
          placeholder={t('supportBundle.descriptionPlaceholder')}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('supportBundle.empty')}
        emptyDescription={t('supportBundle.emptyDescription')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.state')}</TH>
              <TH>{t('common.progress')}</TH>
              <TH>{t('common.error')}</TH>
              <TH className="text-right">{t('common.download')}</TH>
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((b) => {
              const download =
                b.links?.download ||
                (b as { imageURL?: string }).imageURL ||
                b.actions?.download
              return (
                <TR key={b.id ?? b.name}>
                  <TD className="font-medium">{b.name ?? b.id}</TD>
                  <TD>
                    <Badge tone={stateTone(b.state)}>{b.state ?? '—'}</Badge>
                  </TD>
                  <TD className="tabular-nums">{b.progress != null ? `${b.progress}%` : '—'}</TD>
                  <TD className="max-w-xs truncate text-xs">{b.errorMessage ?? '—'}</TD>
                  <TD className="text-right">
                    {download ? (
                      <a
                        href={toHighlandPath(download)}
                        className="inline-flex items-center gap-1 text-sm text-[var(--color-primary)] hover:underline"
                      >
                        <Download size={14} /> {t('common.download')}
                      </a>
                    ) : (
                      '—'
                    )}
                  </TD>
                </TR>
              )
            })}
          </TBody>
        </Table>
      </QueryState>
    </div>
  )
}
