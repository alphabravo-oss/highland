import { beforeEach, describe, expect, it } from 'vitest'
import {
  PREFS_STORAGE_KEY,
  resolveColumns,
  usePreferences,
} from './preferences'

describe('resolveColumns', () => {
  const all = ['name', 'state', 'size', 'actions']

  it('returns all columns when prefs missing', () => {
    expect(resolveColumns({}, 'volumes', all)).toEqual(all)
  })

  it('filters unknown ids and falls back when empty after filter', () => {
    expect(resolveColumns({ volumes: ['name', 'nope'] }, 'volumes', all)).toEqual(['name'])
    expect(resolveColumns({ volumes: ['nope'] }, 'volumes', all)).toEqual(all)
  })
})

describe('usePreferences', () => {
  beforeEach(() => {
    localStorage.removeItem(PREFS_STORAGE_KEY)
    usePreferences.setState({
      density: 'comfortable',
      columnPrefs: {},
      savedViews: [],
    })
  })

  it('toggles density', () => {
    usePreferences.getState().setDensity('compact')
    expect(usePreferences.getState().density).toBe('compact')
  })

  it('sets and resolves columns', () => {
    usePreferences.getState().setColumns('volumes', ['name', 'state'])
    expect(usePreferences.getState().columnPrefs.volumes).toEqual(['name', 'state'])
  })

  it('saves applies and deletes views', () => {
    const id = usePreferences.getState().saveView({
      name: 'Attached only',
      tableId: 'volumes',
      filters: { q: 'attached' },
      columns: ['name', 'state'],
    })
    expect(usePreferences.getState().savedViews).toHaveLength(1)

    const view = usePreferences.getState().applyView(id)
    expect(view?.filters.q).toBe('attached')
    expect(usePreferences.getState().columnPrefs.volumes).toEqual(['name', 'state'])

    usePreferences.getState().deleteView(id)
    expect(usePreferences.getState().savedViews).toHaveLength(0)
  })
})
