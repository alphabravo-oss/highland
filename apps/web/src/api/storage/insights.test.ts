import { describe, expect, it } from 'vitest'
import { buildInsightQuery, storageInsightKeys } from './insights'

describe('storage insight query helpers', () => {
  it('encodes repeated bounded filters without joining values ambiguously', () => {
    const query = buildInsightQuery({
      provider: 'rook/ceph',
      namespace: ['team a', 'team-b'],
      severity: ['warning', 'error'],
      limit: 50,
      authoritativeOnly: true,
      empty: undefined,
    })
    const params = new URLSearchParams(query)
    expect(params.get('provider')).toBe('rook/ceph')
    expect(params.getAll('namespace')).toEqual(['team a', 'team-b'])
    expect(params.getAll('severity')).toEqual(['warning', 'error'])
    expect(params.get('limit')).toBe('50')
    expect(params.get('authoritativeOnly')).toBe('true')
    expect(params.has('empty')).toBe(false)
  })

  it('creates provider- and filter-scoped cache keys', () => {
    const first = storageInsightKeys.capacityForecast('rook-ceph', {
      measure: 'backend-allocated',
      horizon: '7d',
    })
    const second = storageInsightKeys.capacityForecast('longhorn', {
      measure: 'backend-allocated',
      horizon: '7d',
    })
    expect(first).not.toEqual(second)
    expect(storageInsightKeys.timeline({ provider: 'rook-ceph' })).not.toEqual(
      storageInsightKeys.timeline({ provider: 'longhorn' }),
    )
  })
})
