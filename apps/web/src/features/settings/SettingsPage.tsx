import { useMemo, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { useSettings, useUpdateSetting } from '@/api/hooks'
import { useAuth } from '@/auth/AuthContext'
import type { Setting } from '@/api/longhorn'
import { ConfirmDialog } from '@/components/data/ConfirmDialog'
import { PageHeader } from '@/components/data/PageHeader'
import { QueryState } from '@/components/data/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { useAppTranslation } from '@/i18n/useAppTranslation'

const DANGER_CATEGORIES = new Set(['danger zone', 'danger-zone', 'dangerous'])

function isDanger(s: Setting): boolean {
  const cat = (s.definition?.category ?? '').toLowerCase()
  return DANGER_CATEGORIES.has(cat) || cat.includes('danger')
}

type EngineMap = { engines: string[]; values: Record<string, string> }

// Detect data-engine-specific settings: either the definition marks it via
// `dataEngineSpecific`, or the stored value parses as a JSON object with v1/v2
// keys. Returns the per-engine value map, or null to fall back to a single input
// (also when a flagged value cannot be parsed as a JSON object — defensive).
function engineMapOf(s: Setting): EngineMap | null {
  const def = s.definition as (Setting['definition'] & { dataEngineSpecific?: boolean }) | undefined
  const raw = s.value ?? def?.default ?? ''
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    parsed = null
  }
  const isObj = !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
  const hasEngineKeys = isObj && ('v1' in (parsed as object) || 'v2' in (parsed as object))
  if (!def?.dataEngineSpecific && !hasEngineKeys) return null
  if (!isObj) return null
  const values: Record<string, string> = {}
  for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
    values[k] = v == null ? '' : String(v)
  }
  const engines = Object.keys(values)
  if (engines.length === 0) return null
  return { engines, values }
}

export function SettingsPage() {
  const { t } = useAppTranslation()
  const { canMutate } = useAuth()
  const toast = useToast()
  const q = useSettings()
  const updateMut = useUpdateSetting()
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [filter, setFilter] = useState('')
  const [dangerTarget, setDangerTarget] = useState<{ setting: Setting; value: string } | null>(null)

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

  function engineKey(name: string, engine: string): string {
    return `${name}::${engine}`
  }

  function engineValueOf(s: Setting, engine: string, em: EngineMap): string {
    return drafts[engineKey(s.name, engine)] ?? em.values[engine] ?? ''
  }

  // Serialize the per-engine inputs back into the JSON-object string Longhorn expects.
  function serializeEngineMap(s: Setting, em: EngineMap): string {
    const merged: Record<string, string> = {}
    for (const engine of em.engines) {
      merged[engine] = engineValueOf(s, engine, em)
    }
    return JSON.stringify(merged)
  }

  function isEngineDirty(s: Setting, em: EngineMap): boolean {
    return em.engines.some((engine) => {
      const draft = drafts[engineKey(s.name, engine)]
      return draft !== undefined && draft !== (em.values[engine] ?? '')
    })
  }

  async function applySave(s: Setting, value: string, em: EngineMap | null) {
    try {
      await updateMut.mutateAsync({ setting: s, value })
      toast.success(t('settings.saved', { name: s.definition?.displayName || s.name }))
      setDrafts((d) => {
        const next = { ...d }
        delete next[s.name]
        if (em) {
          for (const engine of em.engines) delete next[engineKey(s.name, engine)]
        }
        return next
      })
      setDangerTarget(null)
    } catch (e) {
      toast.error(t('admin.updateFailed'), e instanceof Error ? e.message : undefined)
    }
  }

  function save(s: Setting, value: string, em: EngineMap | null) {
    if (isDanger(s)) {
      setDangerTarget({ setting: s, value })
      return
    }
    void applySave(s, value, em)
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

      <QueryState
        isLoading={q.isLoading}
        error={q.error as Error | null}
        isEmpty={!grouped.length}
        emptyTitle={t('settings.noSettings')}
        skeleton={
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-28 w-full" />
            ))}
          </div>
        }
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
                    const em = engineMapOf(s)
                    const dirty = em
                      ? isEngineDirty(s, em)
                      : drafts[s.name] !== undefined && drafts[s.name] !== (s.value ?? '')
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
                        <div className="space-y-2">
                          {em ? (
                            em.engines.map((engine) => (
                              <div key={engine}>
                                <label className="mb-1 block text-xs font-medium text-[var(--color-muted-foreground)]">
                                  {t('settings.dataEngineLabel', { engine })}
                                </label>
                                {s.definition?.options?.length ? (
                                  <Select
                                    value={engineValueOf(s, engine, em)}
                                    disabled={readOnly}
                                    onChange={(e) =>
                                      setDrafts((d) => ({
                                        ...d,
                                        [engineKey(s.name, engine)]: e.target.value,
                                      }))
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
                                    value={engineValueOf(s, engine, em)}
                                    disabled={readOnly}
                                    onChange={(e) =>
                                      setDrafts((d) => ({
                                        ...d,
                                        [engineKey(s.name, engine)]: e.target.value,
                                      }))
                                    }
                                  />
                                )}
                              </div>
                            ))
                          ) : s.definition?.options?.length ? (
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
                              onClick={() =>
                                void save(s, em ? serializeEngineMap(s, em) : valueOf(s), em)
                              }
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
                name: dangerTarget.setting.definition?.displayName || dangerTarget.setting.name,
                value: dangerTarget.value,
              })
            : undefined
        }
        confirmText={dangerTarget?.setting.name}
        confirmLabel={t('settings.applyDangerous')}
        destructive
        loading={updateMut.isPending}
        error={updateMut.error ? (updateMut.error as Error).message : null}
        onConfirm={async () => {
          if (dangerTarget)
            await applySave(
              dangerTarget.setting,
              dangerTarget.value,
              engineMapOf(dangerTarget.setting),
            )
        }}
      />
    </div>
  )
}
