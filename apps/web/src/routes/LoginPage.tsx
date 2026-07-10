import { Mountain, ShieldCheck } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '@/auth/AuthContext'
import { LocaleSwitcher } from '@/components/layout/LocaleSwitcher'
import { ThemeToggle } from '@/components/layout/ThemeToggle'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type Providers = {
  mode: string
  local: boolean
  oidc: boolean
  oidcMock: boolean
  localAlways: boolean
  message?: string
}

export function LoginPage() {
  const { t } = useAppTranslation()
  const { user, loading, login, loginOidcMock } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [providers, setProviders] = useState<Providers | null>(null)

  useEffect(() => {
    void fetch('/auth/providers')
      .then((r) => r.json())
      .then((p: Providers) => setProviders(p))
      .catch(() =>
        setProviders({
          mode: 'local',
          local: true,
          oidc: false,
          oidcMock: false,
          localAlways: true,
        }),
      )
  }, [])

  if (!loading && user) {
    return <Navigate to="/dashboard" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username, password)
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.loginFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const localOn = providers?.local !== false

  return (
    <div
      className="relative flex min-h-full flex-col bg-[var(--color-background)]"
      data-testid="login-page"
    >
      <div
        className="pointer-events-none absolute inset-0 opacity-60 dark:opacity-40"
        style={{
          background:
            'radial-gradient(ellipse 80% 50% at 50% -20%, oklch(0.7 0.12 255 / 0.25), transparent)',
        }}
      />
      <div className="relative z-10 flex justify-end gap-1 p-4">
        <LocaleSwitcher />
        <ThemeToggle />
      </div>
      <div className="relative z-10 flex flex-1 items-center justify-center px-4 pb-16">
        <div className="w-full max-w-[400px]">
          <div className="mb-8 flex flex-col items-center gap-3 text-center">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-[var(--color-primary)] text-[var(--color-primary-foreground)] shadow-[var(--shadow-md)]">
              <Mountain size={28} strokeWidth={1.75} />
            </div>
            <div>
              <h1 className="text-2xl font-semibold tracking-tight">{t('app.name')}</h1>
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
                {t('app.tagline')}
              </p>
            </div>
          </div>

          <div className="rounded-2xl border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-[var(--shadow-md)]">
            <div className="mb-5 flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)]">
              <ShieldCheck size={14} className="text-[var(--color-primary)]" />
              {t('auth.loginTitle')}
            </div>

            {localOn ? (
              <form className="space-y-4" onSubmit={onSubmit} data-testid="local-login-form">
                <div className="space-y-1.5">
                  <Label htmlFor="username">{t('auth.username')}</Label>
                  <Input
                    id="username"
                    name="username"
                    autoComplete="username"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    required
                    className="h-10"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="password">{t('auth.password')}</Label>
                  <Input
                    id="password"
                    name="password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    className="h-10"
                  />
                </div>
                {error ? (
                  <Alert tone="danger" role="alert">
                    {error}
                  </Alert>
                ) : null}
                <Button type="submit" className="h-10 w-full" disabled={submitting}>
                  {submitting ? t('auth.signingIn') : t('auth.signIn')}
                </Button>
              </form>
            ) : (
              <p className="text-sm text-[var(--color-muted-foreground)]">
                {t('auth.localDisabled')}
              </p>
            )}

            {providers?.oidc || providers?.oidcMock ? (
              <div className="mt-6 border-t border-[var(--color-border)] pt-5">
                <p className="mb-3 text-center text-xs text-[var(--color-muted-foreground)]">
                  {t('auth.enterpriseIdentity')}
                </p>
                {providers.oidc ? (
                  <Button
                    type="button"
                    variant="outline"
                    className="w-full"
                    onClick={() => {
                      window.location.href = '/auth/oidc/start'
                    }}
                  >
                    {t('auth.continueSso')}
                  </Button>
                ) : null}
                {providers.oidcMock ? (
                  <Button
                    type="button"
                    variant="ghost"
                    className="mt-2 w-full"
                    data-testid="oidc-mock-login"
                    onClick={() => {
                      void loginOidcMock('oidc-user@example.com', 'operator').then(() =>
                        navigate('/dashboard', { replace: true }),
                      )
                    }}
                  >
                    {t('auth.ssoMock')}
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>

          <p className="mt-6 text-center text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
            {t('auth.credentialsHint')}
            {import.meta.env.DEV ? (
              <>
                <br />
                {t('auth.devAccounts')}
              </>
            ) : null}
          </p>
        </div>
      </div>
    </div>
  )
}
