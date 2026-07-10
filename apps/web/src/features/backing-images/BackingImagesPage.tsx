import { useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  useBackingImages,
  useCreateBackingImage,
  useDeleteBackingImage,
} from '@/api/hooks'
import { formatBytes, type BackingImage } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function BackingImagesPage() {
  const { t } = useAppTranslation()
  const q = useBackingImages()
  const createMut = useCreateBackingImage()
  const delMut = useDeleteBackingImage()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [sourceType, setSourceType] = useState('download')
  const [url, setUrl] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BackingImage | null>(null)
  const [uploading, setUploading] = useState(false)

  async function createOrUpload() {
    setError(null)
    try {
      if (sourceType === 'upload' && file) {
        setUploading(true)
        // Create metadata first, then stream file to proxy upload path when actions available
        const created = await createMut.mutateAsync({
          name,
          sourceType: 'upload',
          parameters: {},
        })
        const uploadUrl =
          (created as BackingImage).actions?.upload ||
          (created as BackingImage).links?.upload ||
          `/api/v1/lh/backingimages/${encodeURIComponent(name)}`
        const res = await fetch(uploadUrl.startsWith('http') ? uploadUrl : uploadUrl, {
          method: 'POST',
          credentials: 'include',
          body: file,
          headers: {
            'Content-Type': 'application/octet-stream',
          },
        })
        if (!res.ok) {
          throw new Error(`upload failed: ${res.status}`)
        }
        setOpen(false)
        await q.refetch()
        return
      }
      await createMut.mutateAsync({
        name,
        sourceType,
        parameters: sourceType === 'download' ? { url } : {},
      })
      setOpen(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    } finally {
      setUploading(false)
    }
  }

  return (
    <div data-testid="backing-images-page">
      <PageHeader
        title={t('backingImages.title')}
        description={t('backingImages.description')}
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
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.length}
        emptyTitle={t('backingImages.empty')}
        onRetry={() => void q.refetch()}
      >
        <Table>
          <THead>
            <TR>
              <TH>{t('common.name')}</TH>
              <TH>{t('backingImages.uuid')}</TH>
              <TH>{t('common.size')}</TH>
              <TH>{t('backingImages.checksum')}</TH>
              <TH />
            </TR>
          </THead>
          <TBody>
            {(q.data ?? []).map((img) => (
              <TR key={img.id ?? img.name}>
                <TD className="font-medium">{img.name}</TD>
                <TD className="max-w-[10rem] truncate font-mono text-xs">{img.uuid ?? '—'}</TD>
                <TD className="tabular-nums">{formatBytes(img.size)}</TD>
                <TD className="max-w-[12rem] truncate font-mono text-xs">
                  {img.currentChecksum ?? '—'}
                </TD>
                <TD className="text-right">
                  <Button type="button" size="sm" variant="ghost" onClick={() => setDeleteTarget(img)}>
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
        title={t('backingImages.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" disabled={!name || uploading} onClick={() => void createOrUpload()}>
              {uploading ? t('common.uploading') : t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input placeholder={t('common.name')} value={name} onChange={(e) => setName(e.target.value)} />
          <select
            className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3 text-sm"
            value={sourceType}
            onChange={(e) => setSourceType(e.target.value)}
          >
            <option value="download">download</option>
            <option value="upload">{t('backingImages.uploadOption')}</option>
            <option value="clone-from-volume">clone-from-volume</option>
          </select>
          {sourceType === 'download' ? (
            <Input
              placeholder={t('backingImages.imageUrl')}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          ) : null}
          {sourceType === 'upload' ? (
            <Input
              type="file"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          ) : null}
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t('backingImages.delete')}
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
