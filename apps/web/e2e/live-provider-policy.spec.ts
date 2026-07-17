import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

const password = process.env.HIGHLAND_E2E_PASSWORD

async function login(page: Page) {
  await page.goto('/login')
  await page.locator('#username').fill(process.env.HIGHLAND_E2E_USERNAME || 'admin')
  await page.locator('#password').fill(password || '')
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

async function expectNoSeriousA11y(page: Page, label: string) {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  const serious = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
  expect(serious, `${label}: ${JSON.stringify(serious, null, 2)}`).toEqual([])
}

test.describe('live provider-scoped policy qualification', () => {
  test.skip(!password, 'HIGHLAND_E2E_PASSWORD is required for live qualification')

  test('admin policy exposes explicit provider scopes with clean console and a11y', async ({ page }) => {
    await login(page)

    const consoleErrors: string[] = []
    const failedRequests: string[] = []
    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text())
    })
    page.on('requestfailed', (request) => {
      const errorText = request.failure()?.errorText || 'unknown failure'
      // Route changes intentionally close the previous SSE stream and may cancel
      // speculative lazy-route chunks that are no longer needed.
      if (errorText === 'net::ERR_ABORTED') return
      failedRequests.push(`${request.method()} ${request.url()}: ${errorText}`)
    })
    await page.goto('/admin/storage-policy')
    await expect(page.getByTestId('storage-policy-page')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Which providers may common workflows change?' })).toBeVisible()
    await expect(page.getByLabel('Allow common workflows for Longhorn')).toBeVisible()
    await expect(page.getByLabel('Allow common workflows for Rook / Ceph')).toBeVisible()
    await expect(page.getByLabel('Allow common workflows for OpenEBS')).toBeVisible()
    await expect(page.getByText('Longhorn native', { exact: true })).toBeVisible()
    await expect(page.getByText('Rook/Ceph native', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Longhorn + PVC lifecycle' }).click()
    await page.getByRole('button', { name: 'Review policy change' }).click()
    await expect(page.getByRole('dialog', { name: 'Enable storage change capabilities' })).toBeVisible()
    await expect(page.getByText('Required value for this cluster')).toBeVisible()
    await expect(page.getByTestId('required-cluster-identity')).not.toBeEmpty()
    await expect(page.getByRole('button', { name: 'Copy cluster identity' })).toBeVisible()
    await page.getByRole('button', { name: 'Cancel' }).click()
    await page.goto('/storage/operations?provider=longhorn')
    const enabledHeading = page.getByRole('heading', { name: 'Changes are enabled' })
    await expect(enabledHeading).toBeVisible()
    await expect(enabledHeading.locator('svg')).toBeVisible()
    await expectNoSeriousA11y(page, 'provider-scoped admin policy')
    expect(consoleErrors).toEqual([])
    expect(failedRequests).toEqual([])
  })

  test('mobile dark-mode policy and operations pages do not overflow', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await login(page)
    await page.getByTestId('theme-toggle').click()
    await page.getByTestId('theme-toggle').click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    for (const path of ['/admin/storage-policy', '/storage/operations?provider=longhorn', '/storage/operations?provider=rook-ceph', '/storage/operations?provider=openebs']) {
      await page.goto(path)
      await expect(page.locator('main')).toBeVisible()
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
      expect(overflow, `${path} horizontal overflow`).toBeLessThanOrEqual(1)
    }
    await expectNoSeriousA11y(page, 'mobile dark provider operations')
  })
})
