import { useMemo, useState } from 'react'
import { ArrowRight, KeyRound, Plus, ScrollText, Shield, ShieldCheck, Trash2, UserPlus, Users } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { Link } from 'react-router-dom'
import { useAuth } from '@/auth/AuthContext'
import {
  useAuditLog,
  useHighlandUsers,
  usePreflight,
  useUpdateHighlandUser,
} from '@/api/hooks'
import { highlandDelete, highlandPost } from '@/api/client'
import { useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { useToast } from '@/components/ui/toast'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { DataTable } from '@/components/data/DataTable'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type HighlandUser = { username: string; email?: string; role: string; disabled: boolean; mfaEnabled: boolean; mfaRequired: boolean; lastAuthenticatedAt?: string }

export function AdminPage() {
  const { t } = useAppTranslation()
  const { isAdmin } = useAuth()

  if (!isAdmin) {
    return (
      <div data-testid="admin-page">
        <PageHeader title={t('admin.overviewTitle')} description={t('admin.adminRequired')} />
        <EmptyState
          icon={Shield}
          title={t('admin.adminsOnly')}
          description={t('admin.adminsOnlyDescription')}
        />
      </div>
    )
  }

  const destinations = [
    {
      path: '/admin/users',
      title: t('admin.usersTitle'),
      description: t('admin.usersDescription'),
      icon: Users,
    },
    {
      path: '/admin/sso',
      title: t('admin.sso.title'),
      description: t('admin.ssoSummary'),
      icon: KeyRound,
    },
    {
      path: '/admin/security',
      title: 'Authentication security',
      description: 'Configure password requirements and optional or enforced two-factor authentication.',
      icon: ShieldCheck,
    },
    {
      path: '/admin/audit',
      title: t('audit.title'),
      description: t('admin.auditSummary'),
      icon: ScrollText,
    },
    {
      path: '/admin/storage-policy',
      title: 'Storage change policy',
      description: 'Enable or disable bounded storage workflow families with audited confirmation.',
      icon: ShieldCheck,
    },
  ]

  return (
    <div data-testid="admin-page">
      <PageHeader title={t('admin.overviewTitle')} description={t('admin.overviewDescription')} />
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {destinations.map((destination) => {
          const Icon = destination.icon
          return (
            <Link key={destination.path} to={destination.path} className="group rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]">
              <Card className="h-full shadow-[var(--shadow-sm)] transition-colors group-hover:border-[var(--color-primary)]">
                <CardHeader>
                  <div className="flex items-center justify-between gap-3">
                    <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--color-accent)] text-[var(--color-primary)]">
                      <Icon size={18} strokeWidth={1.75} aria-hidden />
                    </span>
                    <ArrowRight size={18} className="text-[var(--color-muted-foreground)] transition-transform group-hover:translate-x-0.5" aria-hidden />
                  </div>
                  <CardTitle className="pt-2">{destination.title}</CardTitle>
                </CardHeader>
                <CardContent className="text-sm text-[var(--color-muted-foreground)]">
                  {destination.description}
                </CardContent>
              </Card>
            </Link>
          )
        })}
      </div>
    </div>
  )
}

export function AdminUsersPage() {
  const { t } = useAppTranslation()
  const { user, isAdmin } = useAuth()
  const users = useHighlandUsers()
  const updateUserMut = useUpdateHighlandUser()
  const qc = useQueryClient()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('operator')
  const [error, setError] = useState<string | null>(null)
  const [deleteUser, setDeleteUser] = useState<string | null>(null)
  const [editUser, setEditUser] = useState<HighlandUser | null>(null)
  const [editRole, setEditRole] = useState('operator')
  const [editEmail, setEditEmail] = useState('')
  const [editDisabled, setEditDisabled] = useState(false)
  const [editResetMfa, setEditResetMfa] = useState(false)
  const [editPassword, setEditPassword] = useState('')
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedUsers, setSelectedUsers] = useState<HighlandUser[]>([])

  const columns = useMemo<ColumnDef<HighlandUser, any>[]>(
    () => [
      {
        id: 'username',
        accessorFn: (u) => u.username,
        header: t('common.username'),
        meta: { className: 'font-medium' },
        cell: ({ row }) => row.original.username,
      },
      {
        id: 'email',
        accessorFn: (u) => u.email ?? '',
        header: 'Email',
        cell: ({ row }) => row.original.email || <span className="text-[var(--color-muted-foreground)]">Not set</span>,
      },
      {
        id: 'role',
        accessorFn: (u) => u.role,
        header: t('admin.role'),
        cell: ({ row }) => (
          <Badge
            tone={
              row.original.role === 'admin'
                ? 'primary'
                : row.original.role === 'operator'
                  ? 'info'
                  : 'default'
            }
          >
            {row.original.role}
          </Badge>
        ),
      },
      {
        id: 'status',
        header: 'Account status',
        accessorFn: (u) => u.disabled ? 'disabled' : 'active',
        cell: ({ row }) => <Badge tone={row.original.disabled ? 'danger' : 'success'}>{row.original.disabled ? 'Disabled' : 'Active'}</Badge>,
      },
      {
        id: 'mfa',
        header: '2FA',
        accessorFn: (u) => u.mfaEnabled ? 'enabled' : u.mfaRequired ? 'required' : 'not enabled',
        cell: ({ row }) => <Badge tone={row.original.mfaEnabled ? 'success' : row.original.mfaRequired ? 'warning' : 'default'}>{row.original.mfaEnabled ? 'Enabled' : row.original.mfaRequired ? 'Required' : 'Not enabled'}</Badge>,
      },
      {
        id: 'lastAuthenticatedAt',
        header: 'Last sign-in',
        accessorFn: (u) => u.lastAuthenticatedAt ?? '',
        cell: ({ row }) => row.original.lastAuthenticatedAt
          ? <span className="whitespace-nowrap text-xs">{new Date(row.original.lastAuthenticatedAt).toLocaleString()}</span>
          : <span className="text-[var(--color-muted-foreground)]">Never</span>,
      },
      {
        id: 'actions',
        header: t('common.actions'),
        enableSorting: false,
        meta: { className: 'text-right', headerClassName: 'text-right' },
        cell: ({ row }) => {
          const u = row.original
          return (
            <div className="flex justify-end gap-1">
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={u.username === user?.username}
                title={u.username === user?.username ? 'Manage your own credentials from My account' : undefined}
                onClick={() => {
                  setEditUser(u)
                  setEditRole(u.role)
                  setEditEmail(u.email ?? '')
                  setEditDisabled(u.disabled)
                  setEditResetMfa(false)
                }}
              >
                {t('common.edit')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                disabled={u.username === user?.username}
                onClick={() => setDeleteUser(u.username)}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          )
        },
      },
    ],
    [t, user?.username],
  )

  async function createUser() {
    setError(null)
    try {
      await highlandPost('/users', { username, email, password, role })
      toast.success(t('admin.userCreated'), username)
      setOpen(false)
      setUsername('')
      setPassword('')
      setEmail('')
      await qc.invalidateQueries({ queryKey: ['users'] })
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  async function updateUser() {
    if (!editUser) return
    try {
      await updateUserMut.mutateAsync({
        username: editUser.username,
        body: { email: editEmail, role: editRole, disabled: editDisabled, resetMfa: editResetMfa, password: editPassword || undefined },
      })
      toast.success(t('admin.userUpdated'), editUser.username)
      setEditUser(null)
      setEditPassword('')
      setEditResetMfa(false)
    } catch (e) {
      toast.error(t('admin.updateFailed'), e instanceof Error ? e.message : undefined)
    }
  }

  if (!isAdmin) {
    return (
      <div data-testid="admin-users-page">
        <PageHeader title={t('admin.usersTitle')} description={t('admin.adminRequired')} />
        <EmptyState
          icon={Shield}
          title={t('admin.adminsOnly')}
          description={t('admin.adminsOnlyDescription')}
        />
      </div>
    )
  }

  const managedUsers = users.data?.data ?? []
  const summary = [
    { label: t('admin.totalAccounts'), value: managedUsers.length, icon: Users, tone: 'text-[var(--color-primary)] bg-[var(--color-accent)]' },
    { label: t('admin.activeAccounts'), value: managedUsers.filter((account) => !account.disabled).length, icon: ShieldCheck, tone: 'text-[var(--color-success)] bg-[var(--color-success)]/10' },
    { label: t('admin.administrators'), value: managedUsers.filter((account) => account.role === 'admin' && !account.disabled).length, icon: Shield, tone: 'text-[var(--color-info)] bg-[var(--color-info)]/10' },
    { label: t('admin.twoFactorEnrolled'), value: managedUsers.filter((account) => account.mfaEnabled).length, icon: KeyRound, tone: 'text-[var(--color-warning)] bg-[var(--color-warning)]/10' },
  ]

  return (
    <div data-testid="admin-users-page">
      <PageHeader
        title={t('admin.title')}
        description={t('admin.description')}
        actions={
          <Button type="button" size="sm" onClick={() => setOpen(true)} data-testid="create-user">
            <UserPlus size={14} /> {t('admin.addUser')}
          </Button>
        }
      />

      <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="User account summary">
        {summary.map(({ label, value, icon: Icon, tone }) => (
          <Card key={label} className="shadow-[var(--shadow-sm)]">
            <CardContent className="flex items-center gap-3 p-4">
              <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${tone}`}>
                <Icon size={18} strokeWidth={1.75} aria-hidden />
              </span>
              <div className="min-w-0">
                <p className="text-2xl font-semibold tabular-nums">{users.isLoading ? '—' : value}</p>
                <p className="truncate text-xs text-[var(--color-muted-foreground)]">{label}</p>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Alert className="mb-4">
        <p>{t('admin.localAccountsScope')}</p>
        <p className="mt-1">{t('admin.currentAccount', { username: user?.username ?? '' })} <Link className="font-medium text-[var(--color-primary)] underline-offset-4 hover:underline" to="/account">{t('admin.manageMyAccount')}</Link></p>
      </Alert>

      <QueryState
        isLoading={users.isLoading}
        error={users.error as Error | null}
        isEmpty={!users.data?.data?.length}
        emptyTitle={t('admin.noUsers')}
        emptyDescription={t('admin.noUsersDescription')}
        onRetry={() => void users.refetch()}
      >
        <DataTable
          data-testid="users-table"
          columns={columns}
          data={managedUsers}
          getRowId={(u) => u.username}
          tableId="users"
          searchable
          enableExport
          exportName="highland-users"
          enableSelection={isAdmin}
          onSelectionChange={setSelectedUsers}
          bulkActions={() => (
            <Button
              type="button"
              size="sm"
              variant="destructive"
              className="h-7 text-xs"
              onClick={() => setBulkDeleteOpen(true)}
            >
              <Trash2 size={14} /> {t('common.delete')}
            </Button>
          )}
        />
      </QueryState>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('admin.addLocalUser')}
        description={t('admin.addLocalUserDescription')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void createUser()} disabled={!username || !password}>
              <Plus size={14} /> {t('common.create')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="nu">{t('common.username')}</Label>
            <Input id="nu" value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ne">Email address <span className="font-normal text-[var(--color-muted-foreground)]">(optional)</span></Label>
            <Input id="ne" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="np">{t('common.password')}</Label>
            <Input
              id="np"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <p className="text-xs text-[var(--color-muted-foreground)]">Use a unique passphrase of at least 15 characters.</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="nr">{t('admin.role')}</Label>
            <Select id="nr" value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="admin">admin</option>
              <option value="operator">operator</option>
              <option value="viewer">viewer</option>
            </Select>
          </div>
          {error ? <Alert tone="danger">{error}</Alert> : null}
        </div>
      </Dialog>

      <Dialog
        open={Boolean(editUser)}
        onOpenChange={(v) => !v && setEditUser(null)}
        title={t('admin.editUser', { username: editUser?.username ?? '' })}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setEditUser(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void updateUser()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="edit-email">Email address</Label>
            <Input id="edit-email" type="email" value={editEmail} onChange={(e) => setEditEmail(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>{t('admin.role')}</Label>
            <Select value={editRole} onChange={(e) => setEditRole(e.target.value)}>
              <option value="admin">admin</option>
              <option value="operator">operator</option>
              <option value="viewer">viewer</option>
            </Select>
          </div>
          <label className="flex items-start gap-3 rounded-md border border-[var(--color-border)] p-3 text-sm">
            <input type="checkbox" className="mt-0.5" checked={editDisabled} disabled={editUser?.username === user?.username} onChange={(e) => setEditDisabled(e.target.checked)} />
            <span><strong>Disable account</strong><span className="block text-[var(--color-muted-foreground)]">Immediately revokes this user’s sessions and prevents sign-in.</span></span>
          </label>
          {editUser?.mfaEnabled ? (
            <label className="flex items-start gap-3 rounded-md border border-[var(--color-border)] p-3 text-sm">
              <input type="checkbox" className="mt-0.5" checked={editResetMfa} onChange={(e) => setEditResetMfa(e.target.checked)} />
              <span><strong>Reset two-factor authentication</strong><span className="block text-[var(--color-muted-foreground)]">Use only after verifying the user through an approved recovery process.</span></span>
            </label>
          ) : null}
          <div className="space-y-1.5">
            <Label>{t('admin.newPasswordOptional')}</Label>
            <Input
              type="password"
              value={editPassword}
              onChange={(e) => setEditPassword(e.target.value)}
              placeholder={t('admin.leaveBlankPassword')}
            />
          </div>
        </div>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteUser)}
        onOpenChange={(v) => !v && setDeleteUser(null)}
        title={t('admin.deleteUser')}
        confirmText={deleteUser ?? undefined}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={async () => {
          if (!deleteUser) return
          await highlandDelete(`/users/${encodeURIComponent(deleteUser)}`)
          toast.success(t('admin.userDeleted'), deleteUser)
          setDeleteUser(null)
          await qc.invalidateQueries({ queryKey: ['users'] })
        }}
      />

      <ConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={(v) => !v && setBulkDeleteOpen(false)}
        title={t('admin.deleteUser')}
        destructive
        confirmLabel={t('common.delete')}
        onConfirm={async () => {
          for (const u of selectedUsers) {
            // Mirror the per-row guard: never delete the current session's user.
            if (u.username === user?.username) continue
            await highlandDelete(`/users/${encodeURIComponent(u.username)}`)
          }
          setBulkDeleteOpen(false)
          await qc.invalidateQueries({ queryKey: ['users'] })
        }}
      />
    </div>
  )
}

type AuditEntry = Record<string, unknown>

export function AuditPage() {
  const { t } = useAppTranslation()
  const { isAdmin } = useAuth()
  const q = useAuditLog()

  const columns = useMemo<ColumnDef<AuditEntry, any>[]>(
    () => [
      {
        id: 'time',
        accessorFn: (e) => String(e.timestamp ?? ''),
        header: t('audit.time'),
        meta: { className: 'whitespace-nowrap text-xs' },
        cell: ({ getValue }) => getValue() as string,
      },
      {
        id: 'user',
        accessorFn: (e) => String(e.username ?? ''),
        header: t('audit.user'),
        cell: ({ getValue }) => getValue() as string,
      },
      {
        id: 'role',
        accessorFn: (e) => String(e.role ?? ''),
        header: t('audit.role'),
        cell: ({ getValue }) => <Badge>{getValue() as string}</Badge>,
      },
      {
        id: 'action',
        accessorFn: (e) => String(e.action ?? e.method ?? ''),
        header: t('audit.action'),
        cell: ({ getValue }) => getValue() as string,
      },
      {
        id: 'result',
        accessorFn: (e) => String(e.result ?? ''),
        header: t('audit.result'),
        cell: ({ getValue }) => getValue() as string,
      },
      {
        id: 'path',
        accessorFn: (e) => String(e.path ?? ''),
        header: t('audit.path'),
        meta: { className: 'max-w-xs truncate font-mono text-xs' },
        cell: ({ getValue }) => getValue() as string,
      },
    ],
    [t],
  )

  if (!isAdmin) {
    return (
      <div data-testid="audit-page">
        <PageHeader title={t('audit.title')} description={t('audit.adminOnly')} />
        <EmptyState
          icon={Shield}
          title={t('admin.adminsOnly')}
          description={t('audit.adminsOnlyDescription')}
        />
      </div>
    )
  }

  return (
    <div data-testid="audit-page">
      <PageHeader
        title={t('audit.title')}
        description={t('audit.descriptionFull')}
      />
      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!q.data?.data?.length}
        emptyTitle={t('audit.emptyYet')}
        emptyDescription={t('audit.emptyDescription')}
        onRetry={() => void q.refetch()}
      >
        <DataTable
          data-testid="audit-table"
          columns={columns}
          data={q.data?.data ?? []}
          getRowId={(e) => String(e.id)}
          tableId="audit"
          searchable
          enableExport
          exportName="highland-audit"
        />
      </QueryState>
    </div>
  )
}

export function PreflightPage() {
  const { t } = useAppTranslation()
  const q = usePreflight()
  return (
    <div data-testid="preflight-page">
      <PageHeader
        title={t('preflight.title')}
        description={t('preflight.description')}
      />
      <QueryState isLoading={q.isLoading} error={q.error as Error | null} onRetry={() => void q.refetch()}>
        <div className="grid gap-2 md:grid-cols-2">
          {(q.data?.checks ?? []).map((c) => (
            <Card key={c.id} className="shadow-[var(--shadow-sm)]">
              <CardContent className="flex items-start justify-between gap-3 py-4 text-sm">
                <div>
                  <div className="font-medium">{c.name}</div>
                  <div className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">{c.detail}</div>
                </div>
                <Badge
                  tone={
                    c.status === 'pass' ? 'success' : c.status === 'skip' ? 'default' : 'warning'
                  }
                >
                  {c.status}
                </Badge>
              </CardContent>
            </Card>
          ))}
        </div>
      </QueryState>
    </div>
  )
}
