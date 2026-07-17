import { ArrowLeft, Check, ShieldCheck } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '@/auth/AuthContext'
import { HighlandLogo } from '@/components/layout/HighlandLogo'
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
  const { user, loading, login, verifyMfa, loginOidcMock } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [providers, setProviders] = useState<Providers | null>(null)
  const [challengeToken, setChallengeToken] = useState<string | null>(null)
  const [verificationCode, setVerificationCode] = useState('')

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
      const result = await login(username, password)
      if (result.mfaRequired) {
        setChallengeToken(result.challengeToken)
        setPassword('')
        return
      }
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.loginFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  async function onVerifyMfa(e: FormEvent) {
    e.preventDefault()
    if (!challengeToken) return
    setError(null)
    setSubmitting(true)
    try {
      await verifyMfa(challengeToken, verificationCode)
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Verification failed')
    } finally {
      setSubmitting(false)
    }
  }

  const localOn = providers?.local !== false
  const features = [t('auth.feature1'), t('auth.feature2'), t('auth.feature3')]

  return (
    <div className="flex min-h-full" data-testid="login-page">
      {/* Brand pane */}
      <aside className="relative hidden w-1/2 flex-col justify-between overflow-hidden bg-[var(--color-primary)] p-12 text-[var(--color-primary-foreground)] lg:flex">
        <div
          className="pointer-events-none absolute inset-0 opacity-90"
          style={{
            background:
              'radial-gradient(ellipse 90% 60% at 15% 0%, rgba(255,255,255,0.18), transparent 60%), radial-gradient(ellipse 80% 80% at 100% 100%, rgba(0,0,0,0.25), transparent 55%)',
          }}
        />
        <div className="relative z-10 flex items-center gap-2">
          <HighlandLogo size={30} />
          <span className="text-lg font-semibold tracking-tight">{t('app.name')}</span>
        </div>

        <div className="relative z-10 max-w-md">
          <h2 className="text-3xl font-semibold leading-tight tracking-tight">
            {t('auth.brandHeadline')}
          </h2>
          <p className="mt-4 text-sm leading-relaxed opacity-90">{t('auth.brandBody')}</p>
          <ul className="mt-8 space-y-3">
            {features.map((f) => (
              <li key={f} className="flex items-center gap-3 text-sm">
                <span className="flex h-5 w-5 items-center justify-center rounded-full bg-white/20">
                  <Check size={13} strokeWidth={2.5} />
                </span>
                {f}
              </li>
            ))}
          </ul>
        </div>

        <div className="relative z-10 text-xs opacity-70">{t('app.tagline')}</div>
      </aside>

      {/* Form pane */}
      <div className="relative flex w-full flex-col bg-[var(--color-background)] lg:w-1/2">
        <div className="flex justify-end gap-1 p-4">
          <LocaleSwitcher />
          <ThemeToggle />
        </div>
        <div className="flex flex-1 items-center justify-center px-4 pb-16">
          <div className="w-full max-w-[400px]">
            {/* compact brand — visible when the brand pane is hidden (mobile) */}
            <div className="mb-8 flex flex-col items-center gap-3 text-center lg:hidden">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-[var(--color-primary)] text-[var(--color-primary-foreground)] shadow-[var(--shadow-md)]">
                <HighlandLogo size={30} />
              </div>
              <div>
                <h1 className="text-2xl font-semibold tracking-tight">{t('app.name')}</h1>
                <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{t('app.tagline')}</p>
              </div>
            </div>

            <div className="mb-6 hidden lg:block">
              <h1 className="text-2xl font-semibold tracking-tight">{t('auth.loginTitle')}</h1>
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">{t('app.tagline')}</p>
            </div>

            <div className="rounded-2xl border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-[var(--shadow-md)]">
              <div className="mb-5 flex items-center gap-2 text-xs font-medium text-[var(--color-muted-foreground)] lg:hidden">
                <ShieldCheck size={14} className="text-[var(--color-primary)]" />
                {t('auth.loginTitle')}
              </div>

              {challengeToken ? (
                <form className="space-y-4" onSubmit={onVerifyMfa} data-testid="mfa-login-form">
                  <div>
                    <h2 className="font-semibold">Two-factor verification</h2>
                    <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
                      Enter the 6-digit code from your authenticator app, or a recovery code.
                    </p>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="verification-code">Verification code</Label>
                    <Input
                      id="verification-code"
                      name="verification-code"
                      autoComplete="one-time-code"
                      inputMode="numeric"
                      value={verificationCode}
                      onChange={(event) => setVerificationCode(event.target.value)}
                      autoFocus
                      required
                      className="h-10 font-mono tracking-widest"
                    />
                  </div>
                  {error ? <Alert tone="danger" role="alert">{error}</Alert> : null}
                  <Button type="submit" className="h-10 w-full" disabled={submitting}>
                    {submitting ? 'Verifying…' : 'Verify and sign in'}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    className="w-full"
                    onClick={() => {
                      setChallengeToken(null)
                      setVerificationCode('')
                      setError(null)
                    }}
                  >
                    <ArrowLeft size={14} /> Back to sign in
                  </Button>
                </form>
              ) : localOn ? (
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
    </div>
  )
}
