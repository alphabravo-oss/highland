import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Browser, type Page } from '@playwright/test'

const adminPassword = process.env.HIGHLAND_E2E_PASSWORD
const viewerPassword = process.env.HIGHLAND_E2E_VIEWER_PASSWORD
const viewerUsername = process.env.HIGHLAND_E2E_VIEWER_USERNAME || 'linstor-qualification-viewer'

const routes = [
  '/storage/providers/linstor',
  '/storage/providers/linstor/linstor/components',
  '/storage/providers/linstor/context',
  '/storage/operations?provider=linstor',
  '/storage/providers/linstor/linstor/nodes',
  '/storage/providers/linstor/linstor/storage-pools',
  '/storage/providers/linstor/linstor/resource-groups',
  '/storage/providers/linstor/linstor/resource-definitions',
  '/storage/providers/linstor/linstor/resources',
  '/storage/providers/linstor/linstor/snapshots',
  '/storage/providers/linstor/linstor/remotes',
  '/storage/providers/linstor/linstor/schedules',
  '/storage/providers/linstor/linstor/error-reports',
  '/storage/providers/linstor/linstor/clusters',
  '/storage/providers/linstor/linstor/satellites',
  '/storage/providers/linstor/linstor/satellite-configurations',
  '/storage/providers/linstor/linstor/node-connections',
  '/storage/classes?provider=linstor',
  '/storage/claims?provider=linstor',
  '/storage/volumes?provider=linstor',
  '/storage/snapshots?provider=linstor',
  '/storage/attachments?provider=linstor',
  '/storage/capacity?provider=linstor',
  '/storage/events?provider=linstor',
] as const

async function login(page: Page, username: string, password: string) {
  await page.goto('/login')
  await page.locator('#username').fill(username)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

async function mutateUser(page: Page, method: 'POST' | 'DELETE', body?: object) {
  return page.evaluate(async ({ method, body, viewerUsername }) => {
    const csrf = document.cookie
      .split('; ')
      .find((entry) => entry.startsWith('highland_csrf='))
      ?.slice('highland_csrf='.length) ?? ''
    const path = method === 'POST' ? '/api/v1/users' : `/api/v1/users/${encodeURIComponent(viewerUsername)}`
    const response = await fetch(path, {
      method,
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrf,
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return { status: response.status, text: await response.text() }
  }, { method, body, viewerUsername })
}

async function qualifyMatrix(browser: Browser, role: 'admin' | 'viewer', username: string, password: string) {
  for (const theme of ['light', 'dark'] as const) {
    for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport })
      await context.addInitScript((selectedTheme) => localStorage.setItem('highland-theme', selectedTheme), theme)
      const page = await context.newPage()
      const consoleErrors: string[] = []
      const requestFailures: string[] = []
      const apiErrors: string[] = []
      page.on('console', (message) => {
        if (message.type() === 'error') consoleErrors.push(message.text())
      })
      page.on('requestfailed', (request) => {
        if (request.failure()?.errorText === 'net::ERR_ABORTED') return
        requestFailures.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText ?? 'unknown'}`)
      })
      page.on('response', (response) => {
        if (response.url().includes('/api/v1/') && response.status() >= 400) {
          apiErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`)
        }
      })

      await login(page, username, password)
      // The anonymous `/auth/me` probe on the login screen is expected to
      // return 401. Qualification starts after the authenticated shell exists.
      consoleErrors.length = 0
      requestFailures.length = 0
      apiErrors.length = 0
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      if (role === 'viewer') {
        await expect(page.getByTestId('sidebar').getByRole('link', { name: 'Administration', exact: true })).toHaveCount(0)
      }

      for (const route of routes) {
        const response = await page.goto(route, { waitUntil: 'domcontentloaded' })
        expect(response?.status(), `${role}/${theme}/${viewport.width}: ${route}`).toBeLessThan(400)
        await expect(page.getByTestId('app-shell')).toBeVisible()
        await expect(page.getByRole('main')).toBeVisible()
        await expect(page.getByText('Something went wrong')).toHaveCount(0)
        await page.waitForTimeout(150)
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
        expect(overflow, `${role}/${theme}/${viewport.width}: ${route} horizontal overflow`).toBeLessThanOrEqual(1)
        const accessibility = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
          .analyze()
        const serious = accessibility.violations.filter(({ impact }) => impact === 'serious' || impact === 'critical')
        expect(serious, `${role}/${theme}/${viewport.width}: ${route}: ${JSON.stringify(serious, null, 2)}`).toEqual([])
      }

      expect(consoleErrors, `${role}/${theme}/${viewport.width}: console`).toEqual([])
      expect(requestFailures, `${role}/${theme}/${viewport.width}: requests`).toEqual([])
      expect(apiErrors, `${role}/${theme}/${viewport.width}: API`).toEqual([])
      await context.close()
    }
  }
}

test.describe('live Piraeus / LINSTOR qualification', () => {
  test.skip(!adminPassword || !viewerPassword, 'admin and viewer live credentials are required')

  test('qualifies the live provider and every route for both roles, themes, and viewports', async ({ browser }) => {
    test.setTimeout(15 * 60_000)
    const adminContext = await browser.newContext()
    const adminPage = await adminContext.newPage()
    await login(adminPage, process.env.HIGHLAND_E2E_USERNAME || 'admin', adminPassword || '')

    const provider = await adminPage.evaluate(async () => {
      const response = await fetch('/api/v1/storage/providers/linstor', { credentials: 'include' })
      return { status: response.status, body: await response.json() }
    })
    expect(provider.status).toBe(200)
    expect(provider.body.health.status).toBe('ok')

    const users = await adminPage.evaluate(async () => {
      const response = await fetch('/api/v1/users', { credentials: 'include' })
      return response.json() as Promise<{ data: Array<{ username: string }> }>
    })
    if (!users.data.some(({ username }) => username === viewerUsername)) {
      const created = await mutateUser(adminPage, 'POST', { username: viewerUsername, password: viewerPassword, role: 'viewer' })
      expect(created.status, created.text).toBe(201)
    }

    try {
      await qualifyMatrix(browser, 'admin', process.env.HIGHLAND_E2E_USERNAME || 'admin', adminPassword || '')
      await qualifyMatrix(browser, 'viewer', viewerUsername, viewerPassword || '')
    } finally {
      const removed = await mutateUser(adminPage, 'DELETE')
      expect([200, 204, 404], removed.text).toContain(removed.status)
      await adminContext.close()
    }
  })
})
