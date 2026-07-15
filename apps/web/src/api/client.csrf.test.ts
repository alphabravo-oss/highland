import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { lhGet, lhPost, lhDelete } from './client'

// Capture fetch calls to assert the CSRF header behavior of highlandFetch.
function mockFetch() {
  const fn = vi.fn(async () =>
    new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } }),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('CSRF header on unsafe methods', () => {
  beforeEach(() => {
    document.cookie = 'highland_csrf=tok123.sig'
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    document.cookie = 'highland_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  })

  const initOf = (fn: ReturnType<typeof mockFetch>): RequestInit =>
    (fn.mock.calls[0] as unknown as [string, RequestInit])[1] ?? {}

  it('attaches X-CSRF-Token from the cookie on POST', async () => {
    const fn = mockFetch()
    await lhPost('/v1/volumes', { name: 'x' })
    expect(new Headers(initOf(fn).headers).get('X-CSRF-Token')).toBe('tok123.sig')
  })

  it('attaches X-CSRF-Token on DELETE', async () => {
    const fn = mockFetch()
    await lhDelete('/v1/volumes/x')
    expect(new Headers(initOf(fn).headers).get('X-CSRF-Token')).toBe('tok123.sig')
  })

  it('does NOT attach the header on GET', async () => {
    const fn = mockFetch()
    await lhGet('/v1/volumes')
    expect(new Headers(initOf(fn).headers).get('X-CSRF-Token')).toBeNull()
  })
})
