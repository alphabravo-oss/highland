import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { highlandGet, highlandPut } from '@/api/client'
import { useAuth } from '@/auth/AuthContext'
import type { SecurityPolicy } from '@/features/account/AccountPage'
import { PageHeader } from '@/components/data/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { useToast } from '@/components/ui/toast'

export function SecuritySettingsPage() {
  const { isAdmin } = useAuth()
  const toast = useToast()
  const query = useQuery({ queryKey: ['security-policy'], queryFn: () => highlandGet<SecurityPolicy>('/admin/security-policy'), enabled: isAdmin })
  const [policy, setPolicy] = useState<SecurityPolicy | null>(null)
  const [saving, setSaving] = useState(false)
  useEffect(() => { if (query.data) setPolicy(query.data) }, [query.data])

  if (!isAdmin) return <><PageHeader title="Authentication security" description="Administrator access is required." /><EmptyState icon={ShieldCheck} title="Administrators only" description="This page controls organization-wide authentication requirements." /></>
  if (query.error) return <Alert tone="danger">{query.error instanceof Error ? query.error.message : 'Security policy unavailable'}</Alert>
  if (!policy) return <div role="status" className="p-6 text-sm text-[var(--color-muted-foreground)]">Loading security policy…</div>
  const setNumber = (key: keyof SecurityPolicy, value: string) => setPolicy({ ...policy, [key]: Number(value) })
  const save = async () => {
    setSaving(true)
    try {
      const updated = await highlandPut<SecurityPolicy>('/admin/security-policy', policy)
      setPolicy(updated)
      toast.success('Security policy saved', 'The new requirements apply to local Highland accounts.')
    } catch (cause) {
      toast.error('Policy update failed', cause instanceof Error ? cause.message : undefined)
    } finally { setSaving(false) }
  }

  return <div data-testid="security-settings-page">
    <PageHeader title="Authentication security" description="Set organization-wide password and multi-factor authentication requirements." actions={<Button onClick={() => void save()} disabled={saving}>{saving ? 'Saving…' : 'Save policy'}</Button>} />
    <Alert className="mb-4">Changes affect Highland-managed local accounts. Enterprise SSO password and MFA policies remain controlled by your identity provider. Existing passwords are not exposed or automatically replaced.</Alert>
    <div className="grid gap-4 lg:grid-cols-2">
      <Card><CardHeader><CardTitle>Password policy</CardTitle></CardHeader><CardContent className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-1.5"><Label htmlFor="minimum-length">Minimum length</Label><Input id="minimum-length" type="number" min={8} max={256} value={policy.minimumPasswordLength} onChange={(event) => setNumber('minimumPasswordLength', event.target.value)} /></div><div className="space-y-1.5"><Label htmlFor="maximum-length">Maximum length</Label><Input id="maximum-length" type="number" min={64} max={256} value={policy.maximumPasswordLength} onChange={(event) => setNumber('maximumPasswordLength', event.target.value)} /></div></div>
        <div className="space-y-1.5"><Label htmlFor="password-history">Remember previous passwords</Label><Input id="password-history" type="number" min={0} max={24} value={policy.passwordHistory} onChange={(event) => setNumber('passwordHistory', event.target.value)} /></div>
        <label className="flex items-start gap-3 rounded-md border border-[var(--color-border)] p-3 text-sm"><input type="checkbox" className="mt-0.5" checked={policy.blockCommonPasswords} onChange={(event) => setPolicy({ ...policy, blockCommonPasswords: event.target.checked })} /><span><strong>Block common passwords</strong><span className="mt-0.5 block text-[var(--color-muted-foreground)]">Reject known weak, product-specific, and identity-derived passphrases.</span></span></label>
        <p className="text-xs leading-relaxed text-[var(--color-muted-foreground)]">Highland supports spaces and Unicode, allows long passphrases, and intentionally does not impose arbitrary character-class or periodic-rotation rules.</p>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>Two-factor authentication</CardTitle></CardHeader><CardContent className="space-y-4">
        <div className="space-y-1.5"><Label htmlFor="mfa-mode">Enforcement</Label><Select id="mfa-mode" value={policy.mfaMode} onChange={(event) => setPolicy({ ...policy, mfaMode: event.target.value as SecurityPolicy['mfaMode'] })}><option value="disabled">Disabled</option><option value="optional">Available, user chooses</option><option value="required-admins">Required for administrators</option><option value="required-all">Required for every local user</option></Select></div>
        <div className="rounded-md bg-[var(--color-muted)]/50 p-3 text-sm text-[var(--color-muted-foreground)]"><strong className="text-[var(--color-foreground)]">Enforcement behavior</strong><p className="mt-1">When 2FA becomes required, affected users may sign in with their password only long enough to open My account and complete enrollment. Other API and UI functions remain blocked.</p></div>
        <p className="text-xs text-[var(--color-muted-foreground)]">Admins can reset a lost authenticator from User management. Recovery codes are single-use and are shown only during enrollment.</p>
      </CardContent></Card>
    </div>
  </div>
}
