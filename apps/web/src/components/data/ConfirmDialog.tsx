import { useState } from 'react'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert } from '@/components/ui/alert'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  confirmText,
  destructive,
  loading,
  error,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  description?: string
  confirmLabel?: string
  /** If set, user must type this exact string to enable confirm (delete volume pattern). */
  confirmText?: string
  destructive?: boolean
  loading?: boolean
  error?: string | null
  onConfirm: () => void | Promise<void>
}) {
  const { t } = useAppTranslation()
  const [typed, setTyped] = useState('')
  const canConfirm = !confirmText || typed === confirmText
  const resolvedConfirmLabel = confirmLabel ?? t('common.confirm')

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) setTyped('')
        onOpenChange(v)
      }}
      title={title}
      description={description}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant={destructive ? 'destructive' : 'default'}
            disabled={!canConfirm || loading}
            onClick={() => void onConfirm()}
          >
            {loading ? t('common.working') : resolvedConfirmLabel}
          </Button>
        </>
      }
    >
      {confirmText ? (
        <div className="space-y-2">
          <p className="text-sm text-[var(--color-muted-foreground)]">
            {t('confirm.typeToConfirmBefore')}{' '}
            <code className="rounded bg-[var(--color-muted)] px-1">{confirmText}</code>{' '}
            {t('confirm.typeToConfirmAfter')}
          </p>
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
        </div>
      ) : null}
      {error ? <Alert tone="danger" className="mt-3">{error}</Alert> : null}
    </Dialog>
  )
}
