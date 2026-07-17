import { chromium } from 'playwright'

const baseURL = process.env.HIGHLAND_URL
const username = process.env.HIGHLAND_USERNAME
const password = process.env.HIGHLAND_PASSWORD
if (!baseURL || !username || !password) {
  throw new Error('HIGHLAND_URL, HIGHLAND_USERNAME, and HIGHLAND_PASSWORD are required')
}

const browser = await chromium.launch({ headless: true })
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const login = await context.request.post(`${baseURL}/auth/login`, { data: { username, password } })
  if (!login.ok()) throw new Error(`login failed with HTTP ${login.status()}`)

  const page = await context.newPage()
  if (process.env.PERF_EMULATE_FAST_3G === '1') {
    const session = await context.newCDPSession(page)
    await session.send('Network.enable')
    await session.send('Network.emulateNetworkConditions', {
      offline: false,
      latency: 150,
      downloadThroughput: 200_000,
      uploadThroughput: 93_750,
      connectionType: 'cellular3g',
    })
  }

  const assets = []
  page.on('response', (response) => {
    const url = new URL(response.url())
    if (url.origin !== new URL(baseURL).origin) return
    if (url.pathname.startsWith('/assets/')) {
      assets.push({
        path: url.pathname,
        encoding: response.headers()['content-encoding'] ?? '',
        bytes: Number(response.headers()['content-length'] ?? 0),
      })
    }
  })

  const started = Date.now()
  await page.goto(`${baseURL}/benchmarks`, { waitUntil: 'domcontentloaded' })
  await page.getByTestId('benchmarks-page').waitFor()
  await page.locator('[data-testid^="benchmark-target-"]').first().waitFor()
  const usableMs = Date.now() - started
  const paints = await page.evaluate(() =>
    Object.fromEntries(performance.getEntriesByType('paint').map((entry) => [entry.name, entry.startTime])),
  )
  const paths = assets.map((asset) => asset.path)
  const forbidden = paths.filter((path) => /dashcharts|DashboardPage|VolumesPage|tanstack-table/.test(path))
  if (forbidden.length) throw new Error(`Benchmarks loaded unrelated chunks: ${forbidden.join(', ')}`)
  const uncompressed = assets.filter((asset) => /\.(?:js|css)$/.test(asset.path) && asset.bytes > 1_024 && !asset.encoding)
  if (uncompressed.length) throw new Error(`Uncompressed text assets: ${uncompressed.map((asset) => asset.path).join(', ')}`)
  console.log(JSON.stringify({ usableMs, paints, assets }, null, 2))
  if (process.env.PERF_EMULATE_FAST_3G === '1') {
    if ((paints['first-contentful-paint'] ?? Infinity) >= 1_000) {
      throw new Error(`FCP ${paints['first-contentful-paint']}ms exceeds 1000ms`)
    }
    if (usableMs >= 1_500) throw new Error(`Usable route ${usableMs}ms exceeds 1500ms`)
  }
} finally {
  await browser.close()
}
