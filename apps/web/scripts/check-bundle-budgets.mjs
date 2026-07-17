import { readFileSync, readdirSync, statSync } from 'node:fs'
import { basename, join } from 'node:path'
import { gzipSync } from 'node:zlib'

const dist = new URL('../dist/', import.meta.url)
const html = readFileSync(new URL('index.html', dist), 'utf8')
const initialPaths = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+\.(?:js|css))"/g)].map((match) => match[1])
const gzipSize = (path) => gzipSync(readFileSync(new URL(`.${path}`, dist)), { level: 9 }).byteLength
const initialGzip = initialPaths.reduce((sum, path) => sum + gzipSize(path), 0)
const initialBudget = 250 * 1024

if (initialGzip > initialBudget) {
  throw new Error(`Initial compressed assets ${initialGzip} exceed ${initialBudget} bytes`)
}

const assetsDir = new URL('assets/', dist)
const routeBudget = 100 * 1024
const exemptions = [/dashcharts/i]
const oversized = readdirSync(assetsDir)
  .filter((name) => name.endsWith('.js') && !exemptions.some((pattern) => pattern.test(name)))
  .map((name) => ({ name, gzip: gzipSize(`/assets/${name}`), raw: statSync(join(assetsDir.pathname, name)).size }))
  .filter((asset) => asset.gzip > routeBudget)

if (oversized.length) {
  throw new Error(`Compressed JavaScript chunk budget exceeded: ${JSON.stringify(oversized)}`)
}

const report = readdirSync(assetsDir)
  .filter((name) => name.endsWith('.js') || name.endsWith('.css'))
  .map((name) => ({
    asset: basename(name),
    rawBytes: statSync(join(assetsDir.pathname, name)).size,
    gzipBytes: gzipSize(`/assets/${name}`),
  }))
  .sort((a, b) => b.gzipBytes - a.gzipBytes)

console.log(JSON.stringify({ initialGzipBytes: initialGzip, initialBudgetBytes: initialBudget, assets: report }, null, 2))
