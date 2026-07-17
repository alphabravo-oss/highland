import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export const PREFS_STORAGE_KEY = 'highland-ui-prefs'

export type Density = 'comfortable' | 'compact'

type SavedView = {
  id: string
  name: string
  tableId: string
  filters: Record<string, string>
  columns: string[]
}

export type PreferencesState = {
  density: Density
  columnPrefs: Record<string, string[]>
  savedViews: SavedView[]
  setDensity: (density: Density) => void
  setColumns: (tableId: string, cols: string[]) => void
  saveView: (view: {
    id?: string
    name: string
    tableId: string
    filters: Record<string, string>
    columns: string[]
  }) => string
  deleteView: (id: string) => void
  /** Applies column prefs for the view; returns the view so callers can apply filters. */
  applyView: (id: string) => SavedView | undefined
}

function newId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `view-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
}

/** Resolve visible column ids for a table, falling back to allColumns when unset. */
export function resolveColumns(
  columnPrefs: Record<string, string[]>,
  tableId: string,
  allColumnIds: string[],
): string[] {
  const prefs = columnPrefs[tableId]
  if (!prefs?.length) return allColumnIds
  const known = new Set(allColumnIds)
  const filtered = prefs.filter((id) => known.has(id))
  return filtered.length ? filtered : allColumnIds
}

export const usePreferences = create<PreferencesState>()(
  persist(
    (set, get) => ({
      density: 'comfortable',
      columnPrefs: {},
      savedViews: [],

      setDensity: (density) => set({ density }),

      setColumns: (tableId, cols) =>
        set((s) => ({
          columnPrefs: { ...s.columnPrefs, [tableId]: cols },
        })),

      saveView: (view) => {
        const id = view.id ?? newId()
        const entry: SavedView = {
          id,
          name: view.name,
          tableId: view.tableId,
          filters: { ...view.filters },
          columns: [...view.columns],
        }
        set((s) => {
          const idx = s.savedViews.findIndex((v) => v.id === id)
          if (idx >= 0) {
            const next = [...s.savedViews]
            next[idx] = entry
            return { savedViews: next }
          }
          return { savedViews: [...s.savedViews, entry] }
        })
        return id
      },

      deleteView: (id) =>
        set((s) => ({
          savedViews: s.savedViews.filter((v) => v.id !== id),
        })),

      applyView: (id) => {
        const view = get().savedViews.find((v) => v.id === id)
        if (!view) return undefined
        set((s) => ({
          columnPrefs: { ...s.columnPrefs, [view.tableId]: [...view.columns] },
        }))
        return view
      },
    }),
    {
      name: PREFS_STORAGE_KEY,
      partialize: (s) => ({
        density: s.density,
        columnPrefs: s.columnPrefs,
        savedViews: s.savedViews,
      }),
    },
  ),
)
