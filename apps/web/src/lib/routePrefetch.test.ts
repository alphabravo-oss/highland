import { afterEach, describe, expect, it, vi } from 'vitest'
import { prefetchRoute } from './routePrefetch'

describe('routePrefetch', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('ignores routes that do not have an intent prefetch target', () => {
    expect(() => prefetchRoute('/benchmarks')).not.toThrow()
  })

  it('honors reduced-data connections', () => {
    vi.stubGlobal('navigator', { connection: { saveData: true, effectiveType: '4g' } })
    expect(() => prefetchRoute('/dashboard')).not.toThrow()
  })
})
