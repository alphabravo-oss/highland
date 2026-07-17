import { useEffect, useState } from 'react'
import { KeyRound, Save, Shield } from 'lucide-react'
import { useAuth } from '@/auth/AuthContext'
import { highlandGet, highlandPut } from '@/api/client'
import { PageHeader } from '@/components/data/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useToast } from '@/components/ui/toast'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export type OIDCConfigPublic = {
  enabled: boolean
  issuerURL: string
  clientID: string
  redirectURL: string
  roleClaim: string
  secretSet: boolean
  ready?: boolean
  initError?: string
}

export function SSOConfigPage() {
  const { t } = useAppTranslation()
  const { isAdmin } = useAuth()
  const toast = useToast()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [issuerURL, setIssuerURL] = useState('')
  const [clientID, setClientID] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [redirectURL, setRedirectURL] = useState('')
  const [roleClaim, setRoleClaim] = useState('highland_role')
  const [secretSet, setSecretSet] = useState(false)
  const [ready, setReady] = useState(false)
  const [initError, setInitError] = useState<string | undefined>()

  useEffect(() => {
    if (!isAdmin) {
      setLoading(false)
      return
    }
    let cancelled = false
    void (async () => {
      setLoading(true)
      setError(null)
      try {
        const cfg = await highlandGet<OIDCConfigPublic>('/auth/oidc-config')
        if (cancelled) return
        setEnabled(Boolean(cfg.enabled))
        setIssuerURL(cfg.issuerURL ?? '')
        setClientID(cfg.clientID ?? '')
        setRedirectURL(cfg.redirectURL ?? '')
        setRoleClaim(cfg.roleClaim || 'highland_role')
        setSecretSet(Boolean(cfg.secretSet))
        setReady(Boolean(cfg.ready))
        setInitError(cfg.initError)
        setClientSecret('')
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : t('admin.sso.loadFailed'))
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isAdmin, t])

  async function onSave() {
    setSaving(true)
    setError(null)
    try {
      const cfg = await highlandPut<OIDCConfigPublic>('/auth/oidc-config', {
        enabled,
        issuerURL,
        clientID,
        clientSecret: clientSecret || '',
        redirectURL,
        roleClaim: roleClaim || 'highland_role',
      })
      setEnabled(Boolean(cfg.enabled))
      setIssuerURL(cfg.issuerURL ?? '')
      setClientID(cfg.clientID ?? '')
      setRedirectURL(cfg.redirectURL ?? '')
      setRoleClaim(cfg.roleClaim || 'highland_role')
      setSecretSet(Boolean(cfg.secretSet))
      setReady(Boolean(cfg.ready))
      setInitError(cfg.initError)
      setClientSecret('')
      if (cfg.initError) {
        toast.success(
          t('admin.sso.settingsSaved'),
          t('admin.sso.discoveryPending', { error: cfg.initError }),
        )
      } else {
        toast.success(
          t('admin.sso.settingsSaved'),
          cfg.ready ? t('admin.sso.oidcReady') : t('admin.sso.settingsStored'),
        )
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : t('admin.sso.saveFailed')
      setError(msg)
      toast.error(t('admin.sso.saveFailed'), msg)
    } finally {
      setSaving(false)
    }
  }

  if (!isAdmin) {
    return (
      <div data-testid="sso-config-page">
        <PageHeader title={t('admin.sso.title')} description={t('admin.adminRequired')} />
        <EmptyState
          icon={Shield}
          title={t('admin.adminsOnly')}
          description={t('admin.sso.adminsOnlyDescription')}
        />
      </div>
    )
  }

  return (
    <div data-testid="sso-config-page">
      <PageHeader
        title={t('admin.sso.title')}
        description={t('admin.sso.description')}
        actions={
          <Button
            type="button"
            size="sm"
            onClick={() => void onSave()}
            disabled={loading || saving}
            data-testid="sso-save"
          >
            <Save size={14} /> {saving ? t('admin.sso.saving') : t('admin.sso.save')}
          </Button>
        }
      />

      {error ? (
        <Alert tone="danger" className="mb-4">
          {error}
        </Alert>
      ) : null}

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge tone={ready ? 'success' : enabled ? 'warning' : 'default'}>
          {ready
            ? t('admin.sso.providerReady')
            : enabled
              ? t('admin.sso.enabledNotReady')
              : t('admin.sso.disabled')}
        </Badge>
        {secretSet ? <Badge tone="info">{t('admin.sso.secretSet')}</Badge> : null}
      </div>

      {initError ? (
        <Alert tone="warning" className="mb-4">
          {t('admin.sso.providerInit', { error: initError })}
        </Alert>
      ) : null}

      <Card className="max-w-2xl shadow-[var(--shadow-sm)]">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <KeyRound size={18} /> {t('admin.sso.oidcConfig')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {loading ? (
            <p className="text-sm text-[var(--color-muted-foreground)]">{t('common.loading')}</p>
          ) : (
            <>
              <label className="flex cursor-pointer items-center gap-3 text-sm">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-[var(--color-input)]"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                  data-testid="sso-enabled"
                />
                <span>
                  <span className="font-medium">{t('admin.sso.enabled')}</span>
                  <span className="mt-0.5 block text-xs text-[var(--color-muted-foreground)]">
                    {t('admin.sso.enabledHint')}
                  </span>
                </span>
              </label>

              <div className="space-y-1.5">
                <Label htmlFor="sso-issuer">{t('admin.sso.issuer')}</Label>
                <Input
                  id="sso-issuer"
                  value={issuerURL}
                  onChange={(e) => setIssuerURL(e.target.value)}
                  placeholder="https://login.example.com/realms/highland"
                  data-testid="sso-issuer"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="sso-client-id">{t('admin.sso.clientId')}</Label>
                <Input
                  id="sso-client-id"
                  value={clientID}
                  onChange={(e) => setClientID(e.target.value)}
                  placeholder="highland"
                  data-testid="sso-client-id"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="sso-client-secret">{t('admin.sso.clientSecret')}</Label>
                <Input
                  id="sso-client-secret"
                  type="password"
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                  placeholder={t('admin.sso.secretPlaceholder')}
                  autoComplete="new-password"
                  data-testid="sso-client-secret"
                />
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {t('admin.sso.clientSecretHint')}
                </p>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="sso-redirect">{t('admin.sso.redirectUrl')}</Label>
                <Input
                  id="sso-redirect"
                  value={redirectURL}
                  onChange={(e) => setRedirectURL(e.target.value)}
                  placeholder="https://highland.example.com/auth/oidc/callback"
                  data-testid="sso-redirect"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="sso-role-claim">{t('admin.sso.roleClaim')}</Label>
                <Input
                  id="sso-role-claim"
                  value={roleClaim}
                  onChange={(e) => setRoleClaim(e.target.value)}
                  placeholder="highland_role"
                  data-testid="sso-role-claim"
                />
                <p className="text-xs text-[var(--color-muted-foreground)]">
                  {t('admin.sso.roleClaimHint')}
                </p>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
