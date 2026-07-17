import { useState, type ReactNode } from 'react'
import { KeyRound, LockKeyhole, Mail, ShieldCheck, UserRound } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { highlandGet, highlandPost, highlandPut, highlandRequest, type HighlandUser } from '@/api/client'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader } from '@/components/data/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export type SecurityPolicy = {
  minimumPasswordLength: number
  maximumPasswordLength: number
  passwordHistory: number
  blockCommonPasswords: boolean
  mfaMode: 'disabled' | 'optional' | 'required-admins' | 'required-all'
}

type Account = {
  user: HighlandUser & { disabled?: boolean; mfaRequired?: boolean }
  policy: SecurityPolicy
  managedBy: 'highland' | 'oidc'
}

type Enrollment = { secret: string; otpauthUri: string; recoveryCodes: string[] }

export function AccountPage() {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const account = useQuery({ queryKey: ['account'], queryFn: () => highlandGet<Account>('/account') })
  const [dialog, setDialog] = useState<'password' | 'email' | 'enroll' | 'disable' | null>(null)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const close = () => {
    setDialog(null)
    setCurrentPassword('')
    setNewPassword('')
    setCode('')
    setEnrollment(null)
    setError(null)
  }
  const run = async (action: () => Promise<unknown>, reauthenticate = true) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      if (reauthenticate) {
        await logout()
        navigate('/login', { replace: true })
      } else {
        close()
        await account.refetch()
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'The change could not be completed')
    } finally {
      setBusy(false)
    }
  }
  const beginEnrollment = async () => {
    setBusy(true)
    setError(null)
    try {
      setEnrollment(await highlandPost<Enrollment>('/account/mfa/enroll', { currentPassword }))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Enrollment could not be started')
    } finally {
      setBusy(false)
    }
  }

  if (account.isLoading) return <div role="status" className="p-6 text-sm text-[var(--color-muted-foreground)]">Loading account…</div>
  if (account.error || !account.data) return <Alert tone="danger">{account.error instanceof Error ? account.error.message : 'Account unavailable'}</Alert>
  const { user, policy, managedBy } = account.data
  const local = managedBy === 'highland'
  const mfaAvailable = policy.mfaMode !== 'disabled'

  return (
    <div data-testid="account-page">
      <PageHeader title="My account" description="Manage your identity, credentials, and sign-in security." />
      {user.mfaSetupRequired ? (
        <Alert tone="warning" className="mb-4">
          Your administrator requires two-factor authentication. Enroll an authenticator before using the rest of Highland.
        </Alert>
      ) : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2"><UserRound size={16} /> Identity</CardTitle></CardHeader>
          <CardContent className="space-y-3 text-sm">
            <AccountRow label="Username" value={user.username} />
            <AccountRow label="Email" value={user.email || 'Not set'} />
            <AccountRow label="Role" value={<Badge tone={user.role === 'admin' ? 'primary' : 'default'}>{user.role}</Badge>} />
            <AccountRow label="Identity source" value={managedBy === 'oidc' ? 'Enterprise SSO' : 'Highland local'} />
            {local ? <Button variant="outline" size="sm" onClick={() => { setEmail(user.email ?? ''); setDialog('email') }}><Mail size={14} /> Change email</Button> : null}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2"><LockKeyhole size={16} /> Password</CardTitle></CardHeader>
          <CardContent className="space-y-3 text-sm">
            {local ? (
              <>
                <p className="text-[var(--color-muted-foreground)]">Use a unique passphrase between {policy.minimumPasswordLength} and {policy.maximumPasswordLength} characters. Highland remembers the last {policy.passwordHistory} passwords.</p>
                <Button variant="outline" size="sm" onClick={() => setDialog('password')}><KeyRound size={14} /> Change password</Button>
              </>
            ) : <p className="text-[var(--color-muted-foreground)]">Your password and email are managed by your enterprise identity provider.</p>}
          </CardContent>
        </Card>
        <Card className="lg:col-span-2">
          <CardHeader><CardTitle className="flex items-center gap-2"><ShieldCheck size={16} /> Two-factor authentication</CardTitle></CardHeader>
          <CardContent className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
            <div>
              <div className="flex items-center gap-2 font-medium">
                Authenticator app
                <Badge tone={managedBy === 'oidc' || user.mfaEnabled ? 'success' : user.mfaRequired ? 'warning' : 'default'}>{managedBy === 'oidc' ? 'Managed by SSO' : user.mfaEnabled ? 'Enabled' : user.mfaRequired ? 'Required' : 'Not enabled'}</Badge>
              </div>
              <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">Time-based one-time codes work with 1Password, Bitwarden, Google Authenticator, Microsoft Authenticator, and compatible apps.</p>
            </div>
            {local && (mfaAvailable || user.mfaEnabled) ? (
              user.mfaEnabled
                ? <Button variant="outline" onClick={() => setDialog('disable')}>Disable 2FA</Button>
                : <Button onClick={() => setDialog('enroll')}>Set up 2FA</Button>
            ) : null}
          </CardContent>
        </Card>
      </div>

      <Dialog open={dialog === 'password'} onOpenChange={(open) => !open && close()} title="Change password" description="You will sign in again after the password changes." footer={<><Button variant="outline" onClick={close}>Cancel</Button><Button disabled={busy || !currentPassword || !newPassword} onClick={() => void run(() => highlandPut('/account/password', { currentPassword, newPassword }))}>Change password</Button></>}>
        <CredentialFields currentPassword={currentPassword} onCurrent={setCurrentPassword} error={error} />
        <div className="mt-3 space-y-1.5"><Label htmlFor="new-password">New passphrase</Label><Input id="new-password" type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></div>
        <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">{policy.minimumPasswordLength}–{policy.maximumPasswordLength} characters; common and recently used passwords are blocked.</p>
      </Dialog>

      <Dialog open={dialog === 'email'} onOpenChange={(open) => !open && close()} title="Change email" description="Changing your email invalidates existing sessions." footer={<><Button variant="outline" onClick={close}>Cancel</Button><Button disabled={busy || !currentPassword} onClick={() => void run(() => highlandPut('/account/email', { currentPassword, email }))}>Save email</Button></>}>
        <div className="space-y-1.5"><Label htmlFor="account-email">Email address</Label><Input id="account-email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} /></div>
        <div className="mt-3"><CredentialFields currentPassword={currentPassword} onCurrent={setCurrentPassword} error={error} /></div>
      </Dialog>

      <Dialog open={dialog === 'enroll'} onOpenChange={(open) => !open && close()} title="Set up two-factor authentication" description={enrollment ? 'Scan the QR code, save the recovery codes, then verify one code.' : 'Confirm your password to begin.'} footer={enrollment ? <><Button variant="outline" onClick={close}>Cancel</Button><Button disabled={busy || !code} onClick={() => void run(() => highlandPost('/account/mfa/confirm', { code }))}>Enable 2FA</Button></> : <><Button variant="outline" onClick={close}>Cancel</Button><Button disabled={busy || !currentPassword} onClick={() => void beginEnrollment()}>Continue</Button></>}>
        {!enrollment ? <CredentialFields currentPassword={currentPassword} onCurrent={setCurrentPassword} error={error} /> : (
          <div className="space-y-4">
            <div className="mx-auto w-fit rounded-lg bg-white p-3"><QRCodeSVG value={enrollment.otpauthUri} size={180} level="M" /></div>
            <div><Label>Manual setup key</Label><code className="mt-1 block break-all rounded bg-[var(--color-muted)] p-2 text-xs">{enrollment.secret}</code></div>
            <Alert tone="warning"><strong>Save these one-time recovery codes now.</strong><div className="mt-2 grid grid-cols-2 gap-1 font-mono text-xs">{enrollment.recoveryCodes.map((item) => <span key={item}>{item}</span>)}</div></Alert>
            <div className="space-y-1.5"><Label htmlFor="enroll-code">6-digit verification code</Label><Input id="enroll-code" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} /></div>
            {error ? <Alert tone="danger">{error}</Alert> : null}
          </div>
        )}
      </Dialog>

      <Dialog open={dialog === 'disable'} onOpenChange={(open) => !open && close()} title="Disable two-factor authentication" description="Confirm both your password and an authenticator or recovery code." footer={<><Button variant="outline" onClick={close}>Cancel</Button><Button variant="destructive" disabled={busy || !currentPassword || !code} onClick={() => void run(() => highlandRequest('/account/mfa', 'DELETE', { currentPassword, code }))}>Disable 2FA</Button></>}>
        <CredentialFields currentPassword={currentPassword} onCurrent={setCurrentPassword} error={error} />
        <div className="mt-3 space-y-1.5"><Label htmlFor="disable-code">Verification code</Label><Input id="disable-code" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} /></div>
      </Dialog>
    </div>
  )
}

function CredentialFields({ currentPassword, onCurrent, error }: { currentPassword: string; onCurrent: (value: string) => void; error: string | null }) {
  return <><div className="space-y-1.5"><Label htmlFor="current-password">Current password</Label><Input id="current-password" type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => onCurrent(event.target.value)} /></div>{error ? <Alert tone="danger" className="mt-3">{error}</Alert> : null}</>
}

function AccountRow({ label, value }: { label: string; value: ReactNode }) {
  return <div className="flex items-center justify-between gap-4"><span className="text-[var(--color-muted-foreground)]">{label}</span><span className="text-right font-medium">{value}</span></div>
}
