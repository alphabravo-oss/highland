import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// Guard: the CSP script-src hash in nginx.conf must match the inline theme-init
// script in index.html. If someone edits that script without recomputing the
// hash, this fails loudly instead of silently breaking theme init under CSP.
describe('CSP inline-script hash', () => {
  // vitest runs with cwd = apps/web
  const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')
  const nginx = readFileSync(resolve(process.cwd(), 'nginx.conf'), 'utf8')

  it('nginx script-src hash matches the index.html inline script', () => {
    const inline = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)].map(
      (m) => m[1]!,
    )
    expect(inline.length).toBe(1) // exactly one inline script is expected
    const want = 'sha256-' + createHash('sha256').update(inline[0]!).digest('base64')

    const m = nginx.match(/script-src[^;]*'(sha256-[A-Za-z0-9+/=]+)'/)
    expect(m, 'nginx.conf should pin a script-src sha256 hash').not.toBeNull()
    expect(m![1]).toBe(want)
  })
})
