import { useState } from 'react'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { volumeActionLabel, type VolumeActionDef } from './volumeActions'

type Props = {
  open: boolean
  onOpenChange: (v: boolean) => void
  def: VolumeActionDef | null
  hosts?: string[]
  images?: string[]
  replicas?: string[]
  loading?: boolean
  onSubmit: (params: Record<string, unknown>) => void | Promise<void>
}

export function ActionFormDialog({
  open,
  onOpenChange,
  def,
  hosts = [],
  images = [],
  replicas = [],
  loading,
  onSubmit,
}: Props) {
  const { t } = useAppTranslation()
  const [value, setValue] = useState('')
  const [hostId, setHostId] = useState(hosts[0] ?? '')
  const [name, setName] = useState('')
  const [size, setSize] = useState('')
  const [namespace, setNamespace] = useState('default')
  const [selectedReplicas, setSelectedReplicas] = useState<string[]>([])
  const [disableFrontend, setDisableFrontend] = useState(false)

  if (!def) return null
  const d = def

  async function submit() {
    const params: Record<string, unknown> = {}
    if ('needsHost' in d && d.needsHost) {
      params.hostId = hostId
      params.disableFrontend = disableFrontend
      params.attachedBy = ''
      params.attacherType = ''
      params.attachmentID = ''
    }
    if ('needsReplicas' in d && d.needsReplicas) {
      params.names = selectedReplicas
    }
    if ('needsImage' in d && d.needsImage) {
      params.image = value
    }
    if ('needsSize' in d && d.needsSize) {
      params.size = size
    }
    if ('needsClone' in d && d.needsClone) {
      params.name = name
      params.snapshot = value
    }
    if ('needsPv' in d && d.needsPv) {
      params.pvName = name || value
      params.fsType = 'ext4'
    }
    if ('needsPvc' in d && d.needsPvc) {
      params.pvcName = name || value
      params.namespace = namespace
    }
    if ('field' in d && d.field) {
      const f = d.field
      if ('type' in d && d.type === 'number') {
        params[f] = Number(value)
      } else {
        params[f] = value
      }
    }
    await onSubmit(params)
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={volumeActionLabel(t, d.key, d.label)}
      description={t('volumes.actionDescription', { key: d.key, defaultValue: d.key })}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={loading} onClick={() => void submit()} data-testid="action-form-submit">
            {loading ? t('common.working') : t('common.apply')}
          </Button>
        </>
      }
    >
      <div className="space-y-3 text-sm">
        {'needsHost' in d && d.needsHost ? (
          <>
            <label className="block space-y-1">
              <span className="font-medium">{t('common.node')}</span>
              <select
                className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3"
                value={hostId}
                onChange={(e) => setHostId(e.target.value)}
              >
                {hosts.map((h) => (
                  <option key={h} value={h}>
                    {h}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={disableFrontend} onChange={(e) => setDisableFrontend(e.target.checked)} />
              {t('volumes.disableFrontend')}
            </label>
          </>
        ) : null}

        {'needsReplicas' in d && d.needsReplicas ? (
          <div className="space-y-1">
            <span className="font-medium">{t('volumes.replicasToSalvage')}</span>
            {replicas.length === 0 ? (
              <p className="text-[var(--color-muted-foreground)]">{t('volumes.noReplicaNames')}</p>
            ) : (
              replicas.map((r) => (
                <label key={r} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={selectedReplicas.includes(r)}
                    onChange={(e) =>
                      setSelectedReplicas((s) =>
                        e.target.checked ? [...s, r] : s.filter((x) => x !== r),
                      )
                    }
                  />
                  {r}
                </label>
              ))
            )}
          </div>
        ) : null}

        {'needsImage' in d && d.needsImage ? (
          <label className="block space-y-1">
            <span className="font-medium">{t('volumes.engineImage')}</span>
            {images.length ? (
              <select
                className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3"
                value={value}
                onChange={(e) => setValue(e.target.value)}
              >
                <option value="">{t('common.select')}</option>
                {images.map((i) => (
                  <option key={i} value={i}>
                    {i}
                  </option>
                ))}
              </select>
            ) : (
              <Input value={value} onChange={(e) => setValue(e.target.value)} placeholder="longhornio/longhorn-engine:..." />
            )}
          </label>
        ) : null}

        {'needsSize' in d && d.needsSize ? (
          <Input value={size} onChange={(e) => setSize(e.target.value)} placeholder={t('volumes.newSizeBytes')} />
        ) : null}

        {'needsClone' in d && d.needsClone ? (
          <>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('volumes.newVolumeName')} />
            <Input value={value} onChange={(e) => setValue(e.target.value)} placeholder={t('volumes.snapshotOptional')} />
          </>
        ) : null}

        {'needsPv' in d && d.needsPv ? (
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('volumes.pvName')} />
        ) : null}

        {'needsPvc' in d && d.needsPvc ? (
          <>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('volumes.pvcName')} />
            <Input value={namespace} onChange={(e) => setNamespace(e.target.value)} placeholder={t('volumes.namespace')} />
          </>
        ) : null}

        {'options' in d && d.options ? (
          <label className="block space-y-1">
            <span className="font-medium">{'field' in d ? d.field : 'value'}</span>
            <select
              className="flex h-9 w-full rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-3"
              value={value}
              onChange={(e) => setValue(e.target.value)}
            >
              <option value="">{t('common.select')}</option>
              {d.options.map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </select>
          </label>
        ) : null}

        {'type' in d && (d.type === 'number' || d.type === 'text') && !('options' in d) ? (
          <Input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={String('field' in d ? d.field : 'value')}
            type={d.type === 'number' ? 'number' : 'text'}
          />
        ) : null}
      </div>
    </Dialog>
  )
}
