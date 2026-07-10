import { useMemo, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { useSettings, useUpdateSetting } from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import type { Setting } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const DANGER_CATEGORIES = new Set(['danger zone', 'danger-zone', 'dangerous'])

function isDanger(s: Setting): boolean {
  const cat = (s.definition?.category ?? '').toLowerCase()
  return DANGER_CATEGORIES.has(cat) || cat.includes('danger')
}

export function SettingsPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const q = useSettings()
  const updateMut = useUpdateSetting()
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [filter, setFilter] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState<string | null>(null)
  const [dangerTarget, setDangerTarget] = useState<Setting | null>(null)

  const grouped = useMemo(() => {
    const items = q.data ?? []
    const f = filter.trim().toLowerCase()
    const filtered = f
      ? items.filter(
          (s) =>
            s.name.toLowerCase().includes(f) ||
            s.definition?.displayName?.toLowerCase().includes(f) ||
            s.definition?.category?.toLowerCase().includes(f),
        )
      : items
    const map = new Map<string, Setting[]>()
    for (const s of filtered) {
      const cat = s.definition?.category || 'general'
      if (!map.has(cat)) map.set(cat, [])
      map.get(cat)!.push(s)
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [q.data, filter])

  function valueOf(s: Setting): string {
    return drafts[s.name] ?? s.value ?? ''
  }

  async function applySave(s: Setting) {
    setError(null)
    setSaved(null)
    try {
      await updateMut.mutateAsync({ setting: s, value: valueOf(s) })
      setSaved(s.name)
      setDrafts((d) => {
        const next = { ...d }
        delete next[s.name]
        return next
      })
      setDangerTarget(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('admin.updateFailed'))
    }
  }

  function save(s: Setting) {
    if (isDanger(s)) {
      setDangerTarget(s)
      return
    }
    void applySave(s)
  }

  return (
    <div data-testid="settings-page">
      <PageHeader
        title={t('settings.title')}
        description={t('settings.description')}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => void q.refetch()}>
            <RefreshCw size={14} /> {t('common.refresh')}
          </Button>
        }
      />

      <Input
        className="mb-4 max-w-md"
        placeholder={t('settings.filterPlaceholder')}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />

      {error ? (
        <Alert tone="danger" className="mb-3">
          {error}
        </Alert>
      ) : null}
      {saved ? (
        <Alert tone="success" className="mb-3">
          {t('settings.saved', { name: saved })}
        </Alert>
      ) : null}

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!grouped.length}
        emptyTitle={t('settings.noSettings')}
        onRetry={() => void q.refetch()}
      >
        <div className="space-y-4">
          {grouped.map(([category, settings]) => {
            const danger = DANGER_CATEGORIES.has(category.toLowerCase())
            return (
              <Card key={category} className={danger ? 'border-red-500/40' : undefined}>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 capitalize">
                    {category}
                    {danger ? <Badge tone="danger">{t('settings.dangerZone')}</Badge> : null}
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  {settings.map((s) => {
                    const readOnly = s.definition?.readOnly
                    const dirty = drafts[s.name] !== undefined && drafts[s.name] !== (s.value ?? '')
                    return (
                      <div
                        key={s.id ?? s.name}
                        className="grid gap-2 border-b border-[var(--color-border)] pb-4 last:border-0 last:pb-0 md:grid-cols-[1fr_1.2fr_auto]"
                      >
                        <div>
                          <div className="text-sm font-medium">
                            {s.definition?.displayName || s.name}
                          </div>
                          <div className="font-mono text-xs text-[var(--color-muted-foreground)]">
                            {s.name}
                          </div>
                          {s.definition?.description ? (
                            <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                              {s.definition.description}
                            </p>
                          ) : null}
                        </div>
                        <div>
                          {s.definition?.options?.length ? (
                            <Select
                              value={valueOf(s)}
                              disabled={readOnly}
                              onChange={(e) =>
                                setDrafts((d) => ({ ...d, [s.name]: e.target.value }))
                              }
                            >
                              {s.definition.options.map((o) => (
                                <option key={o} value={o}>
                                  {o}
                                </option>
                              ))}
                            </Select>
                          ) : (
                            <Input
                              value={valueOf(s)}
                              disabled={readOnly}
                              onChange={(e) =>
                                setDrafts((d) => ({ ...d, [s.name]: e.target.value }))
                              }
                            />
                          )}
                        </div>
                        {canMutate ? (
                          <div className="flex items-start">
                            <Button
                              type="button"
                              size="sm"
                              disabled={readOnly || !dirty || updateMut.isPending}
                              onClick={() => void save(s)}
                            >
                              <Save size={14} /> {t('common.save')}
                            </Button>
                          </div>
                        ) : null}
                      </div>
                    )
                  })}
                </CardContent>
              </Card>
            )
          })}
        </div>
      </QueryState>

      <ConfirmDialog
        open={Boolean(dangerTarget)}
        onOpenChange={(v) => !v && setDangerTarget(null)}
        title={t('settings.dangerTitle')}
        description={
          dangerTarget
            ? t('settings.dangerDescription', {
                name: dangerTarget.definition?.displayName || dangerTarget.name,
                value: valueOf(dangerTarget),
              })
            : undefined
        }
        confirmText={dangerTarget?.name}
        confirmLabel={t('settings.applyDangerous')}
        destructive
        loading={updateMut.isPending}
        error={updateMut.error ? (updateMut.error as Error).message : null}
        onConfirm={async () => {
          if (dangerTarget) await applySave(dangerTarget)
        }}
      />
    </div>
  )
}
