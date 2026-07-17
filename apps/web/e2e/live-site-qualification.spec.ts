import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

const live = process.env.HIGHLAND_LIVE_E2E === '1'
const username = process.env.HIGHLAND_E2E_USERNAME ?? ''
const password = process.env.HIGHLAND_E2E_PASSWORD ?? ''

const workspaceRoots = [
  '/dashboard',
  '/storage/providers/rook-ceph',
  '/storage/providers/openebs',
  '/storage/providers/linstor',
] as const

const dashboardSections = [
  'Operational signals',
  'Capacity & resilience',
  'Kubernetes consumption',
  'Provider resources',
] as const

async function login(page: Page) {
  await page.goto('/login')
  await page.locator('#username').fill(username)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

test.describe('live site qualification', () => {
  test.skip(!live || !username || !password, 'live cluster credentials are required')

  test('all navigable pages are wired, accessible, and operational', async ({ page }) => {
    test.setTimeout(10 * 60_000)
    const consoleErrors: string[] = []
    const pageErrors: string[] = []
    const requestFailures: string[] = []
    const apiErrors: string[] = []

    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text())
    })
    page.on('pageerror', (error) => pageErrors.push(error.message))
    page.on('requestfailed', (request) => {
      if (request.failure()?.errorText === 'net::ERR_ABORTED') return
      requestFailures.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText ?? 'unknown'}`)
    })
    page.on('response', (response) => {
      if (response.url().includes('/api/v1/') && response.status() >= 400) {
        apiErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`)
      }
    })

    await login(page)
    // The anonymous session probe on the login route is intentionally 401.
    consoleErrors.length = 0
    pageErrors.length = 0
    requestFailures.length = 0
    apiErrors.length = 0

    const routes = new Set<string>(['/account', '/admin', '/admin/users', '/admin/security', '/admin/sso', '/admin/audit', '/admin/storage-policy'])
    for (const root of workspaceRoots) {
      await page.goto(root, { waitUntil: 'domcontentloaded' })
      await expect(page.getByRole('main').getByRole('heading', { level: 1 }), `${route} page heading`).toBeVisible()
      const statusSummary = page.getByRole('main').getByRole('heading', { level: 2 }).first()
      await expect(statusSummary).toBeVisible()
      const sectionPositions: number[] = []
      for (const heading of dashboardSections) {
        const section = page.getByRole('main').getByRole('heading', { name: heading, exact: true })
        await expect(section).toBeVisible()
        sectionPositions.push((await section.boundingBox())?.y ?? -1)
      }
      expect(sectionPositions, `${root} dashboard section order`).toEqual([...sectionPositions].sort((a, b) => a - b))
      expect((await statusSummary.boundingBox())?.y ?? -1, `${root} status placement`).toBeLessThan(sectionPositions[0])
      const links = await page.getByTestId('sidebar').locator('a[href]').evaluateAll((anchors) =>
        anchors.map((anchor) => (anchor as HTMLAnchorElement).getAttribute('href') ?? ''),
      )
      for (const href of links) {
        if (href.startsWith('/')) routes.add(href)
      }
    }

    for (const route of [...routes].sort()) {
      const response = await page.goto(route, { waitUntil: 'domcontentloaded' })
      expect(response?.status(), route).toBeLessThan(400)
      await expect(page.getByTestId('app-shell')).toBeVisible()
      await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toBeVisible()
      await expect(page.getByText('Something went wrong')).toHaveCount(0)
      await expect(page.locator('.placeholder-page')).toHaveCount(0)
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      )
      expect(overflow, `${route} horizontal overflow`).toBeLessThanOrEqual(1)
      // Allow page queries to either complete or be canceled before moving on;
      // otherwise late retries could escape the final error assertions.
      await page.waitForTimeout(500)
    }
    await page.waitForTimeout(2_000)

    const accessibility = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze()
    const serious = accessibility.violations.filter(
      ({ impact }) => impact === 'serious' || impact === 'critical',
    )
    expect(serious, JSON.stringify(serious, null, 2)).toEqual([])
    expect(consoleErrors).toEqual([])
    expect(pageErrors).toEqual([])
    expect(requestFailures).toEqual([])
    expect(apiErrors).toEqual([])
  })
})
