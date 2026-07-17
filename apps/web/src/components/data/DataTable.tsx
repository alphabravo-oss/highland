import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsUpDown,
  ChevronUp,
  Columns3,
  Download,
  Search,
  X,
} from 'lucide-react'
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type Column,
  type ColumnDef,
  type OnChangeFn,
  type RowData,
  type RowSelectionState,
  type SortingState,
  type VisibilityState,
} from '@tanstack/react-table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Table, TBody, TD, TH, THead, TR } from '@/components/ui/table'
import { useAppTranslation } from '@/i18n/useAppTranslation'
import { cn } from '@/lib/utils'
import { usePreferences, type Density } from '@/store/preferences'

// Allow columns to carry style hints used by the shared table primitives.
declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    /** Applied to both the header cell and body cells for a column. */
    className?: string
    /** Applied only to the header cell (in addition to `className`). */
    headerClassName?: string
    /** Human label used by the Columns menu / CSV export when the header is not a plain string. */
    exportLabel?: string
  }
}

const PAGE_SIZES = [10, 25, 50, 100] as const
const DEFAULT_PAGE_SIZE = 25

export type DataTableProps<T> = {
  columns: ColumnDef<T, any>[]
  data: T[]
  /** Controlled column visibility (e.g. from ColumnPicker / preferences store). */
  columnVisibility?: VisibilityState
  /** Enables the built-in per-page column menu + persistence (when columnVisibility is not controlled). */
  tableId?: string
  /** Controlled global search string. Rows are filtered by TanStack's global filter. */
  globalFilter?: string
  /** Renders a built-in search box (used when globalFilter is not controlled). */
  searchable?: boolean
  searchPlaceholder?: string
  /** Shows a CSV export button for the (filtered) rows. */
  enableExport?: boolean
  exportName?: string
  /** Adds a leading checkbox column and enables row selection. */
  enableSelection?: boolean
  /** Rendered in a bar above the table when one or more rows are selected. */
  bulkActions?: (rows: T[]) => ReactNode
  /** Extra controls placed in the toolbar (e.g. a Create button). */
  toolbarExtra?: ReactNode
  /** Replaces the generic no-results message when the current data set is empty. */
  emptyState?: ReactNode
  /** Notified with the selected row originals whenever selection changes. */
  onSelectionChange?: (rows: T[]) => void
  /** Controlled selection state (keyed by getRowId). Falls back to internal state. */
  rowSelection?: RowSelectionState
  onRowSelectionChange?: OnChangeFn<RowSelectionState>
  /** Stable row id — used as the selection key. */
  getRowId?: (row: T, index: number) => string
  /** Overrides the preferences-store density. */
  density?: Density
  initialPageSize?: number
  'data-testid'?: string
}

function SortIcon({ dir }: { dir: false | 'asc' | 'desc' }) {
  if (dir === 'asc') return <ChevronUp size={14} strokeWidth={2} aria-hidden />
  if (dir === 'desc') return <ChevronDown size={14} strokeWidth={2} aria-hidden />
  return <ChevronsUpDown size={14} strokeWidth={1.5} className="opacity-50" aria-hidden />
}

function columnLabel<T>(col: Column<T, unknown>): string {
  const meta = col.columnDef.meta
  if (meta?.exportLabel) return meta.exportLabel
  const h = col.columnDef.header
  if (typeof h === 'string') return h
  return col.id
}

function loadVisibility(tableId?: string): VisibilityState {
  if (!tableId || typeof localStorage === 'undefined') return {}
  try {
    return JSON.parse(localStorage.getItem(`highland-cols:${tableId}`) || '{}') as VisibilityState
  } catch {
    return {}
  }
}
function saveVisibility(tableId: string, vis: VisibilityState) {
  try {
    localStorage.setItem(`highland-cols:${tableId}`, JSON.stringify(vis))
  } catch {
    /* ignore quota/serialization errors */
  }
}

function csvCell(value: unknown): string {
  const s = value == null ? '' : String(value)
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

export function DataTable<T>({
  columns,
  data,
  columnVisibility,
  tableId,
  globalFilter,
  searchable = false,
  searchPlaceholder,
  enableExport = false,
  exportName = 'export',
  enableSelection = false,
  bulkActions,
  toolbarExtra,
  emptyState,
  onSelectionChange,
  rowSelection,
  onRowSelectionChange,
  getRowId,
  density,
  initialPageSize = DEFAULT_PAGE_SIZE,
  'data-testid': testId,
}: DataTableProps<T>) {
  const { t } = useAppTranslation()
  const storeDensity = usePreferences((s) => s.density)
  const effectiveDensity = density ?? storeDensity
  const cellPad = effectiveDensity === 'compact' ? 'py-1' : 'py-2.5'

  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: initialPageSize })
  const [internalSelection, setInternalSelection] = useState<RowSelectionState>({})
  const [internalSearch, setInternalSearch] = useState('')

  // Column visibility: a controlled prop wins; otherwise persist per tableId.
  const managedVisibility = Boolean(tableId) && columnVisibility === undefined
  const [visState, setVisState] = useState<VisibilityState>(() =>
    managedVisibility ? loadVisibility(tableId) : {},
  )
  const effectiveVisibility = columnVisibility ?? (managedVisibility ? visState : {})

  const selection = rowSelection ?? internalSelection
  const handleSelectionChange: OnChangeFn<RowSelectionState> =
    onRowSelectionChange ?? setInternalSelection

  const search = globalFilter ?? (searchable ? internalSearch : '')

  const tableColumns = useMemo<ColumnDef<T, any>[]>(() => {
    if (!enableSelection) return columns
    const selectColumn: ColumnDef<T, any> = {
      id: 'select',
      enableSorting: false,
      enableHiding: false,
      meta: { className: 'w-8' },
      header: ({ table }) => (
        <input
          type="checkbox"
          className="size-3.5 accent-[var(--color-primary)]"
          aria-label={t('common.select')}
          checked={table.getIsAllPageRowsSelected()}
          ref={(el) => {
            if (el) {
              el.indeterminate =
                table.getIsSomePageRowsSelected() && !table.getIsAllPageRowsSelected()
            }
          }}
          onChange={table.getToggleAllPageRowsSelectedHandler()}
        />
      ),
      cell: ({ row }) => (
        <input
          type="checkbox"
          className="size-3.5 accent-[var(--color-primary)]"
          checked={row.getIsSelected()}
          disabled={!row.getCanSelect()}
          onChange={row.getToggleSelectedHandler()}
          aria-label={t('common.selectItem', { name: getRowId ? getRowId(row.original, row.index) : row.id })}
        />
      ),
    }
    return [selectColumn, ...columns]
  }, [columns, enableSelection, getRowId, t])

  const table = useReactTable({
    data,
    columns: tableColumns,
    state: {
      sorting,
      pagination,
      columnVisibility: effectiveVisibility,
      globalFilter: search,
      rowSelection: selection,
    },
    enableRowSelection: enableSelection,
    getRowId,
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: managedVisibility
      ? (updater) => {
          setVisState((old) => {
            const next = typeof updater === 'function' ? updater(old) : updater
            if (tableId) saveVisibility(tableId, next)
            return next
          })
        }
      : undefined,
    onRowSelectionChange: handleSelectionChange,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  // Reset to the first page whenever the search changes.
  useEffect(() => {
    setPagination((p) => (p.pageIndex > 0 ? { ...p, pageIndex: 0 } : p))
  }, [search])

  useEffect(() => {
    if (!onSelectionChange) return
    onSelectionChange(table.getSelectedRowModel().rows.map((r) => r.original))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, data])

  const visibleColCount = table.getVisibleLeafColumns().length
  const filteredCount = table.getFilteredRowModel().rows.length
  const pageRows = table.getRowModel().rows
  const { pageIndex, pageSize } = table.getState().pagination
  const pageCount = Math.max(1, table.getPageCount())
  const start = filteredCount === 0 ? 0 : pageIndex * pageSize + 1
  const end = Math.min(filteredCount, (pageIndex + 1) * pageSize)
  const selectedRows = enableSelection ? table.getSelectedRowModel().rows.map((r) => r.original) : []
  const selectedCount = selectedRows.length

  const hideableColumns = table.getAllLeafColumns().filter((c) => c.getCanHide() && c.id !== 'select')
  const showColumnsMenu = managedVisibility && hideableColumns.length > 0

  function exportCsv() {
    const cols = table.getVisibleLeafColumns().filter((c) => c.accessorFn != null)
    const header = cols.map((c) => csvCell(columnLabel(c)))
    const rows = table.getFilteredRowModel().rows.map((r) => cols.map((c) => csvCell(r.getValue(c.id))))
    const csv = [header, ...rows].map((r) => r.join(',')).join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${exportName}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  const showToolbar =
    (searchable && globalFilter === undefined) || showColumnsMenu || enableExport || Boolean(toolbarExtra)

  return (
    <div data-testid={testId}>
      {showToolbar ? (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          {searchable && globalFilter === undefined ? (
            <div className="relative min-w-[200px] flex-1 sm:max-w-xs">
              <Search
                size={14}
                className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--color-muted-foreground)]"
              />
              <Input
                value={internalSearch}
                onChange={(e) => setInternalSearch(e.target.value)}
                placeholder={searchPlaceholder ?? t('table.searchPlaceholder')}
                className="h-8 pl-8"
                data-testid="table-search"
              />
            </div>
          ) : (
            <div className="flex-1" />
          )}
          {toolbarExtra}
          {showColumnsMenu ? (
            <Popover>
              <PopoverTrigger asChild>
                <Button type="button" size="sm" variant="outline" className="h-8 gap-1.5 text-xs" data-testid="table-columns">
                  <Columns3 size={14} strokeWidth={1.75} />
                  {t('table.columns')}
                </Button>
              </PopoverTrigger>
              <PopoverContent align="end" className="w-48 p-1.5">
                <div className="flex flex-col">
                  {hideableColumns.map((col) => (
                    <label
                      key={col.id}
                      className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-[var(--color-accent)]"
                    >
                      <input
                        type="checkbox"
                        className="size-3.5 accent-[var(--color-primary)]"
                        checked={col.getIsVisible()}
                        onChange={col.getToggleVisibilityHandler()}
                      />
                      <span>{columnLabel(col)}</span>
                    </label>
                  ))}
                </div>
              </PopoverContent>
            </Popover>
          ) : null}
          {enableExport ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 gap-1.5 text-xs"
              onClick={exportCsv}
              data-testid="table-export"
            >
              <Download size={14} strokeWidth={1.75} />
              {t('table.export')}
            </Button>
          ) : null}
        </div>
      ) : null}

      {selectedCount > 0 && bulkActions ? (
        <div
          className="mb-3 flex flex-wrap items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2"
          data-testid="bulk-action-bar"
        >
          <span className="text-sm font-medium">{t('table.selectedCount', { count: selectedCount })}</span>
          <div className="flex flex-wrap items-center gap-2">{bulkActions(selectedRows)}</div>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="ml-auto h-7 gap-1 text-xs"
            onClick={() => table.resetRowSelection()}
          >
            <X size={13} /> {t('common.clear')}
          </Button>
        </div>
      ) : null}

      <Table data-density={effectiveDensity}>
        <THead className="sticky top-0 z-10 bg-[var(--color-muted)]">
          {table.getHeaderGroups().map((headerGroup) => (
            <TR key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                const meta = header.column.columnDef.meta
                const canSort = header.column.getCanSort()
                const content = header.isPlaceholder
                  ? null
                  : flexRender(header.column.columnDef.header, header.getContext())
                return (
                  <TH key={header.id} className={cn(meta?.className, meta?.headerClassName)}>
                    {canSort ? (
                      <button
                        type="button"
                        onClick={header.column.getToggleSortingHandler()}
                        className="inline-flex items-center gap-1 uppercase tracking-wide hover:text-[var(--color-foreground)]"
                      >
                        {content}
                        <SortIcon dir={header.column.getIsSorted()} />
                      </button>
                    ) : (
                      content
                    )}
                  </TH>
                )
              })}
            </TR>
          ))}
        </THead>
        <TBody>
          {pageRows.length === 0 ? (
            <TR>
              <TD
                colSpan={visibleColCount}
                className="py-8 text-center text-[var(--color-muted-foreground)]"
              >
                {emptyState ?? t('table.noResults')}
              </TD>
            </TR>
          ) : (
            pageRows.map((row) => (
              <TR key={row.id} data-state={row.getIsSelected() ? 'selected' : undefined}>
                {row.getVisibleCells().map((cell) => (
                  <TD
                    key={cell.id}
                    className={cn(cellPad, cell.column.columnDef.meta?.className)}
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TD>
                ))}
              </TR>
            ))
          )}
        </TBody>
      </Table>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2">
            <span className="text-[var(--color-muted-foreground)]">{t('table.rowsPerPage')}</span>
            <select
              className="h-8 rounded-md border border-[var(--color-input)] bg-[var(--color-background)] px-2 text-sm"
              value={pageSize}
              onChange={(e) => table.setPageSize(Number(e.target.value))}
              data-testid="table-page-size"
            >
              {PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <span className="tabular-nums text-[var(--color-muted-foreground)]" data-testid="table-range">
            {start}–{end} {t('table.of')} {filteredCount}
          </span>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 gap-1"
              onClick={() => table.previousPage()}
              disabled={!table.getCanPreviousPage()}
              data-testid="table-prev"
            >
              <ChevronLeft size={14} aria-hidden />
              <span className="hidden sm:inline">{t('table.previous')}</span>
            </Button>
            <span className="px-1 tabular-nums text-[var(--color-muted-foreground)]">
              {t('table.page', { page: pageIndex + 1, total: pageCount })}
            </span>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 gap-1"
              onClick={() => table.nextPage()}
              disabled={!table.getCanNextPage()}
              data-testid="table-next"
            >
              <span className="hidden sm:inline">{t('table.next')}</span>
              <ChevronRight size={14} aria-hidden />
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
