import { expect, test } from '@playwright/test'

const live = process.env.HIGHLAND_LIVE_E2E === '1'
const username = process.env.HIGHLAND_E2E_USERNAME ?? ''
const password = process.env.HIGHLAND_E2E_PASSWORD ?? ''

test.describe('live storage context control plane', () => {
  test.skip(!live, 'set HIGHLAND_LIVE_E2E=1 for an explicitly configured live cluster')

  test('loads provider context and insight surfaces without browser errors', async ({ page }) => {
    const browserErrors: string[] = []
    page.on('console', (message) => {
      if (message.type() === 'error') browserErrors.push(message.text())
    })
    page.on('pageerror', (error) => browserErrors.push(error.message))

    await page.goto('/login')
    await page.locator('#username').fill(username)
    await page.locator('#password').fill(password)
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByTestId('app-shell')).toBeVisible()
    // The login route intentionally probes the current session before one
    // exists; only errors after authentication indicate a live UI regression.
    browserErrors.length = 0

    await page.goto('/storage/providers/rook-ceph/context')
    await expect(page.getByTestId('provider-context-page')).toBeVisible()
    await expect(page.getByTestId('relationship-panel')).toBeVisible()
    await expect(page.getByTestId('drift-panel')).toBeVisible()
    await expect(page.getByTestId('storage-timeline-panel')).toBeVisible()
    await expect(page.getByTestId('capacity-ownership-panel')).toBeVisible()
    await expect(page.getByTestId('provider-comparison-panel')).toBeVisible()

    await page.goto('/storage/insights')
    await expect(page.getByTestId('storage-insights-page')).toBeVisible()
    await expect(page.getByTestId('storage-timeline-panel')).toBeVisible()
    await expect(page.getByTestId('capacity-ownership-panel')).toBeVisible()
    await expect(page.getByTestId('provider-comparison-panel')).toBeVisible()

    expect(browserErrors).toEqual([])
  })
})
