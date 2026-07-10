import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import es from './locales/es.json'
import { LOCALE_STORAGE_KEY, SUPPORTED_LOCALES } from './index'

/** Collect leaf key paths from a nested translation object. */
function keyPaths(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix ? [prefix] : []
  }
  const entries = Object.entries(value as Record<string, unknown>)
  if (entries.length === 0) {
    return prefix ? [prefix] : []
  }
  return entries.flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return keyPaths(child, path)
  })
}

describe('i18n locales', () => {
  it('uses highland-locale localStorage key', () => {
    expect(LOCALE_STORAGE_KEY).toBe('highland-locale')
  })

  it('supports en and es', () => {
    expect(SUPPORTED_LOCALES).toEqual(['en', 'es'])
  })

  it('en and es have the same key paths', () => {
    const enKeys = keyPaths(en).sort()
    const esKeys = keyPaths(es).sort()
    expect(enKeys).toEqual(esKeys)
    expect(enKeys.length).toBeGreaterThan(20)
  })

  it('covers required top-level namespaces', () => {
    const required = [
      'app',
      'nav',
      'common',
      'auth',
      'dashboard',
      'volumes',
      'volumeDetail',
      'volumeActions',
      'nodes',
      'backups',
      'backupTargets',
      'recurringJobs',
      'systemBackups',
      'backingImages',
      'engineImages',
      'instanceManagers',
      'orphans',
      'supportBundle',
      'performance',
      'settings',
      'admin',
      'audit',
      'preflight',
      'notFound',
      'tablePrefs',
      'theme',
      'locale',
    ]
    for (const ns of required) {
      expect(en).toHaveProperty(ns)
      expect(es).toHaveProperty(ns)
    }
  })
})
