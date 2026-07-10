import { describe, expect, it } from 'vitest'
import { formatBytes, hasAction, parseSizeToBytes, type LHResource } from './longhorn'
import { toHighlandPath } from './client'

describe('toHighlandPath', () => {
  it('rewrites /v1 paths to proxy prefix', () => {
    expect(toHighlandPath('/v1/volumes')).toBe('/api/v1/lh/volumes')
    expect(toHighlandPath('/v1/volumes/vol-1?action=attach')).toBe(
      '/api/v1/lh/volumes/vol-1?action=attach',
    )
  })

  it('leaves highland paths unchanged', () => {
    expect(toHighlandPath('/api/v1/lh/nodes')).toBe('/api/v1/lh/nodes')
  })
})

describe('parseSizeToBytes', () => {
  it('parses Gi units', () => {
    expect(parseSizeToBytes('10Gi')).toBe(String(10 * 1024 ** 3))
  })

  it('parses bare bytes', () => {
    expect(parseSizeToBytes('1024')).toBe('1024')
  })
})

describe('formatBytes', () => {
  it('formats gibibytes', () => {
    expect(formatBytes(1024 ** 3)).toContain('GiB')
  })
})

describe('hasAction', () => {
  it('reads manager actions map', () => {
    const r: LHResource = {
      id: 'v1',
      type: 'volume',
      actions: { attach: '/api/v1/lh/volumes/v1?action=attach' },
    }
    expect(hasAction(r, 'attach')).toBe(true)
    expect(hasAction(r, 'detach')).toBe(false)
  })
})
