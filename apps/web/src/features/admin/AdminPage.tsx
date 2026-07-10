import { useState } from 'react'
import { Plus, Shield, Trash2, UserPlus } from 'lucide-react'
import { useAuth } from '@/auth/AuthContext'
import {
  useAuditLog,
  useCompatibility,
  useHighlandUsers,
  usePreflight,
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
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useToast } from '@/components/ui/toast'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function AdminPage() {
  const { t } = useAppTranslation()
  const { user, isAdmin } = useAuth()
  const users = useHighlandUsers()
  const compat = useCompatibility()
  const qc = useQueryClient()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('operator')
  const [error, setError] = useState<string | null>(null)
  const [deleteUser, setDeleteUser] = useState<string | null>(null)
  const [editUser, setEditUser] = useState<{ username: string; role: string } | null>(null)
  const [editRole, setEditRole] = useState('operator')
  const [editPassword, setEditPassword] = useState('')

  async function createUser() {
    setError(null)
    try {
      await highlandPost('/users', { username, password, role })
      toast.success(t('admin.userCreated'), username)
      setOpen(false)
      setUsername('')
      setPassword('')
      await qc.invalidateQueries({ queryKey: ['users'] })
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.createFailed'))
    }
  }

  async function updateUser() {
    if (!editUser) return
    try {
      // use fetch put via highland API
      const res = await fetch(`/api/v1/users/${encodeURIComponent(editUser.username)}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role: editRole, password: editPassword || undefined }),
      })
      if (!res.ok) {
        const b = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(b.error ?? res.statusText)
      }
      toast.success(t('admin.userUpdated'), editUser.username)
      setEditUser(null)
      setEditPassword('')
      await qc.invalidateQueries({ queryKey: ['users'] })
    } catch (e) {
      toast.error(t('admin.updateFailed'), e instanceof Error ? e.message : undefined)
    }
  }

  if (!isAdmin) {
    return (
      <div data-testid="admin-page">
        <PageHeader title={t('admin.usersTitle')} description={t('admin.adminRequired')} />
        <EmptyState
          icon={Shield}
          title={t('admin.adminsOnly')}
          description={t('admin.adminsOnlyDescription')}
        />
      </div>
    )
  }

  return (
    <div data-testid="admin-page">
      <PageHeader
        title={t('admin.title')}
        description={t('admin.description')}
        actions={
          <Button type="button" size="sm" onClick={() => setOpen(true)} data-testid="create-user">
            <UserPlus size={14} /> {t('admin.addUser')}
          </Button>
        }
      />

      <div className="mb-4 grid gap-4 lg:grid-cols-3">
        <Card className="shadow-[var(--shadow-sm)]">
          <CardHeader>
            <CardTitle>{t('admin.yourSession')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between gap-2">
              <span className="text-[var(--color-muted-foreground)]">{t('common.user')}</span>
              <span className="font-medium">{user?.username}</span>
            </div>
            <div className="flex justify-between gap-2">
              <span className="text-[var(--color-muted-foreground)]">{t('admin.role')}</span>
              <Badge tone="primary">{user?.role}</Badge>
            </div>
          </CardContent>
        </Card>
        <Card className="shadow-[var(--shadow-sm)] lg:col-span-2">
          <CardHeader>
            <CardTitle>{t('admin.platform')}</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-1 font-mono text-xs text-[var(--color-muted-foreground)] sm:grid-cols-2">
            {compat.data
              ? Object.entries(compat.data).map(([k, v]) => (
                  <div key={k}>
                    <span className="text-[var(--color-foreground)]">{k}</span>: {JSON.stringify(v)}
                  </div>
                ))
              : t('common.loading')}
          </CardContent>
        </Card>
      </div>

      <QueryState
        isLoading={users.isLoading}
        error={users.error as Error | null}
        isEmpty={!users.data?.data?.length}
        emptyTitle={t('admin.noUsers')}
        emptyDescription={t('admin.noUsersDescription')}
        onRetry={() => void users.refetch()}
      >
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] shadow-[var(--shadow-sm)]">
          <Table>
            <THead>
              <TR>
                <TH>{t('common.username')}</TH>
                <TH>{t('admin.role')}</TH>
                <TH className="text-right">{t('common.actions')}</TH>
              </TR>
            </THead>
            <TBody>
              {(users.data?.data ?? []).map((u) => (
                <TR key={u.username}>
                  <TD className="font-medium">{u.username}</TD>
                  <TD>
                    <Badge
                      tone={
                        u.role === 'admin' ? 'primary' : u.role === 'operator' ? 'info' : 'default'
                      }
                    >
                      {u.role}
                    </Badge>
                  </TD>
                  <TD className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setEditUser(u)
                          setEditRole(u.role)
                        }}
                      >
                        {t('common.edit')}
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        disabled={u.username === user?.username}
                        onClick={() => setDeleteUser(u.username)}
                      >
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </div>
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
            <Label htmlFor="np">{t('common.password')}</Label>
            <Input
              id="np"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
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
            <Label>{t('admin.role')}</Label>
            <Select value={editRole} onChange={(e) => setEditRole(e.target.value)}>
              <option value="admin">admin</option>
              <option value="operator">operator</option>
              <option value="viewer">viewer</option>
            </Select>
          </div>
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
    </div>
  )
}

export function AuditPage() {
  const { t } = useAppTranslation()
  const { isAdmin } = useAuth()
  const q = useAuditLog()

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
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] shadow-[var(--shadow-sm)]">
          <Table>
            <THead>
              <TR>
                <TH>{t('audit.time')}</TH>
                <TH>{t('audit.user')}</TH>
                <TH>{t('audit.role')}</TH>
                <TH>{t('audit.action')}</TH>
                <TH>{t('audit.result')}</TH>
                <TH>{t('audit.path')}</TH>
              </TR>
            </THead>
            <TBody>
              {(q.data?.data ?? []).map((e) => (
                <TR key={String(e.id)}>
                  <TD className="whitespace-nowrap text-xs">{String(e.timestamp ?? '')}</TD>
                  <TD>{String(e.username ?? '')}</TD>
                  <TD>
                    <Badge>{String(e.role ?? '')}</Badge>
                  </TD>
                  <TD>{String(e.action ?? e.method ?? '')}</TD>
                  <TD>{String(e.result ?? '')}</TD>
                  <TD className="max-w-xs truncate font-mono text-xs">{String(e.path ?? '')}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </div>
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
