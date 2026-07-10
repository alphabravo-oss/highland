import { useState } from 'react'
import { Check, Database, HardDriveDownload, Cloud } from 'lucide-react'
import { useCreateBackupCredential, useCreateBackupTarget } from '@/api/hooks'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type Backend = 's3' | 'nfs' | 'azure'

const BACKENDS: Array<{ id: Backend; icon: typeof Cloud; labelKey: string; descKey: string }> = [
  { id: 's3', icon: Cloud, labelKey: 'backupWizard.s3', descKey: 'backupWizard.s3Desc' },
  { id: 'nfs', icon: HardDriveDownload, labelKey: 'backupWizard.nfs', descKey: 'backupWizard.nfsDesc' },
  { id: 'azure', icon: Database, labelKey: 'backupWizard.azure', descKey: 'backupWizard.azureDesc' },
]

function dnsSafe(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '')
}

export function BackupSetupWizard({
  open,
  onOpenChange,
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onDone: () => void
}) {
  const { t } = useAppTranslation()
  const createCred = useCreateBackupCredential()
  const createTarget = useCreateBackupTarget()

  const [step, setStep] = useState(1)
  const [backend, setBackend] = useState<Backend>('s3')
  const [name, setName] = useState('default')
  const [poll, setPoll] = useState('300')
  const [error, setError] = useState<string | null>(null)
  // s3
  const [bucket, setBucket] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [endpoint, setEndpoint] = useState('')
  const [accessKey, setAccessKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  // nfs
  const [nfsServer, setNfsServer] = useState('')
  const [nfsPath, setNfsPath] = useState('')
  // azure
  const [azContainer, setAzContainer] = useState('')
  const [azAccount, setAzAccount] = useState('')
  const [azKey, setAzKey] = useState('')

  function reset() {
    setStep(1); setBackend('s3'); setName('default'); setPoll('300'); setError(null)
    setBucket(''); setRegion('us-east-1'); setEndpoint(''); setAccessKey(''); setSecretKey('')
    setNfsServer(''); setNfsPath(''); setAzContainer(''); setAzAccount(''); setAzKey('')
  }

  function buildTarget(): { url: string; secretName?: string; secretData?: Record<string, string> } {
    if (backend === 's3') {
      const url = `s3://${bucket}@${region}/`
      const data: Record<string, string> = { AWS_ACCESS_KEY_ID: accessKey, AWS_SECRET_ACCESS_KEY: secretKey }
      if (endpoint.trim()) data.AWS_ENDPOINTS = endpoint.trim()
      return { url, secretName: `${dnsSafe(name)}-s3-credential`, secretData: data }
    }
    if (backend === 'azure') {
      const url = `azblob://${azContainer}@blob.core.windows.net/`
      return {
        url,
        secretName: `${dnsSafe(name)}-azblob-credential`,
        secretData: { AZBLOB_ACCOUNT_NAME: azAccount, AZBLOB_ACCOUNT_KEY: azKey },
      }
    }
    // nfs — no credentials
    const path = nfsPath.startsWith('/') ? nfsPath : `/${nfsPath}`
    return { url: `nfs://${nfsServer}:${path}` }
  }

  async function finish() {
    setError(null)
    try {
      const { url, secretName, secretData } = buildTarget()
      if (secretName && secretData) {
        await createCred.mutateAsync({ name: secretName, data: secretData })
      }
      await createTarget.mutateAsync({
        name,
        backupTargetURL: url,
        credentialSecret: secretName,
        pollInterval: `${poll}s`,
      })
      onDone()
      onOpenChange(false)
      reset()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  const busy = createCred.isPending || createTarget.isPending
  const { url } = buildTarget()

  const step2Valid =
    (backend === 's3' && bucket && accessKey && secretKey) ||
    (backend === 'nfs' && nfsServer && nfsPath) ||
    (backend === 'azure' && azContainer && azAccount && azKey)

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
      title={t('backupWizard.title')}
      description={t('backupWizard.subtitle')}
      className="max-w-lg"
      footer={
        <div className="flex w-full items-center justify-between gap-2">
          <span className="text-xs text-[var(--color-muted-foreground)]">{t('backupWizard.step', { step, total: 3 })}</span>
          <div className="flex gap-2">
            {step > 1 ? (
              <Button type="button" variant="outline" onClick={() => setStep((s) => s - 1)} disabled={busy}>
                {t('backupWizard.back')}
              </Button>
            ) : null}
            {step < 3 ? (
              <Button type="button" onClick={() => setStep((s) => s + 1)} disabled={step === 2 && !step2Valid}>
                {t('backupWizard.next')}
              </Button>
            ) : (
              <Button type="button" onClick={() => void finish()} disabled={busy || !name}>
                {busy ? t('backupWizard.creating') : t('backupWizard.finish')}
              </Button>
            )}
          </div>
        </div>
      }
    >
      {error ? <Alert tone="danger" className="mb-3">{error}</Alert> : null}

      {step === 1 ? (
        <div className="grid gap-2">
          {BACKENDS.map((be) => {
            const Icon = be.icon
            const active = backend === be.id
            return (
              <button
                key={be.id}
                type="button"
                onClick={() => setBackend(be.id)}
                className={cn(
                  'flex items-center gap-3 rounded-lg border p-3 text-left transition-colors',
                  active
                    ? 'border-[var(--color-primary)] bg-[var(--color-accent,rgba(120,120,120,0.06))]'
                    : 'border-[var(--color-border)] hover:bg-[var(--color-accent,rgba(120,120,120,0.06))]',
                )}
              >
                <Icon size={20} className="text-[var(--color-primary)]" />
                <div className="flex-1">
                  <div className="text-sm font-medium">{t(be.labelKey)}</div>
                  <div className="text-xs text-[var(--color-muted-foreground)]">{t(be.descKey)}</div>
                </div>
                {active ? <Check size={16} className="text-[var(--color-primary)]" /> : null}
              </button>
            )
          })}
        </div>
      ) : null}

      {step === 2 ? (
        <div className="space-y-3">
          {backend === 's3' ? (
            <>
              <Field label={t('backupWizard.bucket')} value={bucket} onChange={setBucket} placeholder="my-longhorn-backups" />
              <Field label={t('backupWizard.region')} value={region} onChange={setRegion} placeholder="us-east-1" />
              <Field label={t('backupWizard.endpoint')} value={endpoint} onChange={setEndpoint} placeholder="https://minio.example.com (optional, for S3-compatible)" />
              <Field label={t('backupWizard.accessKey')} value={accessKey} onChange={setAccessKey} />
              <Field label={t('backupWizard.secretKey')} value={secretKey} onChange={setSecretKey} type="password" />
            </>
          ) : backend === 'nfs' ? (
            <>
              <Field label={t('backupWizard.nfsServer')} value={nfsServer} onChange={setNfsServer} placeholder="10.0.0.5" />
              <Field label={t('backupWizard.nfsPath')} value={nfsPath} onChange={setNfsPath} placeholder="/exports/longhorn" />
            </>
          ) : (
            <>
              <Field label={t('backupWizard.azContainer')} value={azContainer} onChange={setAzContainer} placeholder="longhorn-backups" />
              <Field label={t('backupWizard.azAccount')} value={azAccount} onChange={setAzAccount} />
              <Field label={t('backupWizard.azKey')} value={azKey} onChange={setAzKey} type="password" />
            </>
          )}
        </div>
      ) : null}

      {step === 3 ? (
        <div className="space-y-3">
          <Field label={t('backupWizard.name')} value={name} onChange={setName} placeholder="default" />
          <Field label={t('backupWizard.poll')} value={poll} onChange={setPoll} type="number" />
          <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-muted)] p-3 text-sm">
            <div className="mb-1 font-medium">{t('backupWizard.review')}</div>
            <div className="break-all font-mono text-xs text-[var(--color-muted-foreground)]">{url}</div>
            {backend !== 'nfs' ? (
              <div className="mt-1 text-xs text-[var(--color-muted-foreground)]">{t('backupWizard.credNote')}</div>
            ) : null}
          </div>
        </div>
      ) : null}
    </Dialog>
  )
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <div className="space-y-1">
      <Label>{label}</Label>
      <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} type={type} />
    </div>
  )
}
