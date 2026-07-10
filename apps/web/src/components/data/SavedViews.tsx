import { useMemo, useState } from 'react'
import { Bookmark, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { resolveColumns, usePreferences } from '@/store/preferences'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function SavedViews({
  tableId,
  filters,
  allColumnIds,
  onApplyFilters,
  className,
}: {
  tableId: string
  /** Current filter snapshot to persist with a new view. */
  filters: Record<string, string>
  /** Canonical column ids used when no prefs exist yet. */
  allColumnIds: string[]
  /** Called after store applyView so the page can restore filter state. */
  onApplyFilters?: (filters: Record<string, string>) => void
  className?: string
}) {
  const { t } = useAppTranslation()
  const [name, setName] = useState('')
  const columnPrefs = usePreferences((s) => s.columnPrefs)
  const savedViews = usePreferences((s) => s.savedViews)
  const saveView = usePreferences((s) => s.saveView)
  const deleteView = usePreferences((s) => s.deleteView)
  const applyView = usePreferences((s) => s.applyView)

  const views = useMemo(
    () => savedViews.filter((v) => v.tableId === tableId),
    [savedViews, tableId],
  )

  function onSave() {
    const trimmed = name.trim()
    if (!trimmed) return
    const columns = resolveColumns(columnPrefs, tableId, allColumnIds)
    saveView({
      name: trimmed,
      tableId,
      filters: { ...filters },
      columns,
    })
    setName('')
  }

  function onApply(id: string) {
    const view = applyView(id)
    if (!view) return
    onApplyFilters?.(view.filters)
  }

  return (
    <div
      className={cn('flex flex-wrap items-center gap-2', className)}
      data-testid="saved-views"
    >
      <div className="flex items-center gap-1">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('tablePrefs.saveViewAs')}
          className="h-8 w-36 text-xs"
          aria-label={t('tablePrefs.newViewName')}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              onSave()
            }
          }}
        />
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="h-8 gap-1"
          disabled={!name.trim()}
          onClick={onSave}
          title={t('tablePrefs.saveCurrentView')}
        >
          <Bookmark size={14} strokeWidth={1.75} />
          {t('common.save')}
        </Button>
      </div>

      {views.length > 0 ? (
        <ul className="flex flex-wrap items-center gap-1">
          {views.map((v) => (
            <li
              key={v.id}
              className="inline-flex items-center gap-0.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] pl-1"
            >
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 px-2 text-xs"
                onClick={() => onApply(v.id)}
                title={t('tablePrefs.applyView', { name: v.name })}
              >
                {v.name}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-7 w-7 px-0 text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]"
                aria-label={t('tablePrefs.deleteView', { name: v.name })}
                onClick={() => deleteView(v.id)}
              >
                <Trash2 size={12} />
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
