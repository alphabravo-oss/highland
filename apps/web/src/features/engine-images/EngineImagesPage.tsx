import { useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  useCreateEngineImage,
  useDeleteEngineImage,
  useEngineImages,
} from '@/api/hooks'
import type { EngineImage } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge, stateTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function EngineImagesPage() {
  const { t } = useAppTranslation()
  const q = useEngineImages()
  const createMut = useCreateEngineImage()
  const delMut = useDeleteEngineImage()
  const [open, setOpen] = useState(false)
  const [image, setImage] = useState('longhornio/longhorn-engine:v1.12.0')
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<EngineImage | null>(null)

  return (
    <div data-testid="engine-images-page">
      <PageHeader
        title={t('engineImages.title')}
        description={t('engineImages.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
              <RefreshCw size={14} /> {t('common.refresh')}
            </Button>
            <Button type="button" size="sm" onClick={() => setOpen(true)}>
              <Plus size={14} /> {t('common.deploy')}
            </Button>
          </>
        }
      />
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('engineImages.empty')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('common.image')}</TH>
              <TH>{t('common.state')}</TH>
              <TH>{t('common.default')}</TH>
              <TH>{t('common.refs')}</TH>
              <TH />
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((img) => (
              <TR key={img.id ?? img.name}>
                <TD className="font-medium">{img.name}</TD>
                <TD className="max-w-xs truncate font-mono text-xs">{img.image}</TD>
                <TD>
                  <Badge tone={stateTone(img.state)}>{img.state ?? '—'}</Badge>
                </TD>
                <TD>{img.default ? t('common.yes') : t('common.no')}</TD>
                <TD className="tabular-nums">{img.refCount ?? '—'}</TD>
                <TD className="text-right">
                  {!img.default ? (
                    <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(img)}>
                      <Trash2 size={14} />
                    </Button>
                  ) : null}
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('engineImages.deploy')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setError(null)
                void createMut
                  .mutateAsync({ image })
                  .then(() => setOpen(false))
                  .catch((e: Error) => setError(e.message))
              }}
            >
              {t('common.deploy')}
            </Button>
          </>
        }
      >
        <Input value={image} onChange={(e) => setImage(e.target.value)} />
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('engineImages.delete')}
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
