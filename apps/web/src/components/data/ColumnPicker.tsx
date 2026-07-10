import { Columns3 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { resolveColumns, usePreferences } from '@/store/preferences'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export type ColumnOption = {
  id: string
  label: string
}

export function ColumnPicker({
  tableId,
  allColumns,
  className,
}: {
  tableId: string
  allColumns: ColumnOption[]
  className?: string
}) {
  const { t } = useAppTranslation()
  const columnPrefs = usePreferences((s) => s.columnPrefs)
  const setColumns = usePreferences((s) => s.setColumns)
  const allIds = allColumns.map((c) => c.id)
  const visible = resolveColumns(columnPrefs, tableId, allIds)
  const visibleSet = new Set(visible)

  function toggle(id: string) {
    if (visibleSet.has(id)) {
      if (visible.length <= 1) return
      setColumns(
        tableId,
        visible.filter((c) => c !== id),
      )
      return
    }
    // Re-insert in canonical order from allColumns
    const next = allIds.filter((c) => c === id || visibleSet.has(c))
    setColumns(tableId, next)
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className={cn('h-8 gap-1.5 text-xs', className)}
          data-testid="column-picker"
        >
          <Columns3 size={14} strokeWidth={1.75} />
          {t('tablePrefs.columns')}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-48 p-1.5" aria-label={t('tablePrefs.visibleColumns')}>
        <div role="group" aria-label={t('tablePrefs.visibleColumns')} className="flex flex-col">
          {allColumns.map((col) => {
            const checked = visibleSet.has(col.id)
            return (
              <label
                key={col.id}
                className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-[var(--color-accent)]"
              >
                <input
                  type="checkbox"
                  className="size-3.5 accent-[var(--color-primary)]"
                  checked={checked}
                  disabled={checked && visible.length <= 1}
                  onChange={() => toggle(col.id)}
                />
                <span>{col.label}</span>
              </label>
            )
          })}
        </div>
      </PopoverContent>
    </Popover>
  )
}
