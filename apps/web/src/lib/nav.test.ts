import { describe, expect, it } from 'vitest'
import { filterNavForRole, findNavItem, navGroups } from './nav'

describe('nav', () => {
  it('exposes grouped nav with Lucide icons and i18n keys for every item', () => {
    expect(navGroups.length).toBeGreaterThan(0)
    for (const group of navGroups) {
      expect(group.label).toBeTruthy()
      expect(group.labelKey.startsWith('nav.')).toBe(true)
      for (const item of group.items) {
        expect(item.path.startsWith('/')).toBe(true)
        // Lucide icons are React components (function or forwardRef object)
        expect(item.icon).toBeTruthy()
        expect(item.label.length).toBeGreaterThan(0)
        expect(item.labelKey.startsWith('nav.')).toBe(true)
      }
    }
  })

  it('finds nav item by pathname', () => {
    const dash = findNavItem('/dashboard')
    expect(dash?.id).toBe('dashboard')
    const vol = findNavItem('/volumes/vol-1')
    expect(vol?.id).toBe('volumes')
  })

  it('includes Overview, Storage, and Admin groups from IA', () => {
    const ids = navGroups.map((g) => g.id)
    expect(ids).toContain('overview')
    expect(ids).toContain('storage')
    expect(ids).toContain('admin')
  })

  it('hides admin nav for viewers', () => {
    const filtered = filterNavForRole(navGroups, 'viewer')
    expect(filtered.map((g) => g.id)).not.toContain('admin')
  })

  it('shows admin nav for admins', () => {
    const filtered = filterNavForRole(navGroups, 'admin')
    expect(filtered.map((g) => g.id)).toContain('admin')
  })
})
