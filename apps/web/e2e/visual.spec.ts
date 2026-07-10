import { expect, test } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const outDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../test-results/visual',
)

async function login(page: import('@playwright/test').Page) {
  await page.goto('/login')
  await expect(page.getByTestId('login-page')).toBeVisible()
  await page.locator('#username').fill('admin')
  await page.locator('#password').fill('highland')
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

test.describe('visual smoke screenshots', () => {
  test('captures login + shell surfaces for regression baselines', async ({ page }) => {
    // Login light
    await page.goto('/login')
    await expect(page.getByTestId('login-page')).toBeVisible()
    await page.screenshot({
      path: path.join(outDir, 'login-light.png'),
      fullPage: true,
    })

    // Login dark
    await page.evaluate(() => {
      document.documentElement.classList.add('dark')
      document.documentElement.dataset.theme = 'dark'
    })
    await page.screenshot({
      path: path.join(outDir, 'login-dark.png'),
      fullPage: true,
    })
    await page.evaluate(() => {
      document.documentElement.classList.remove('dark')
      document.documentElement.dataset.theme = 'light'
    })

    await login(page)

    // Dashboard light
    await expect(page.getByTestId('dashboard-page')).toBeVisible()
    await page.screenshot({
      path: path.join(outDir, 'dashboard-light.png'),
      fullPage: true,
    })

    // Dashboard dark
    await page.evaluate(() => {
      document.documentElement.classList.add('dark')
      document.documentElement.dataset.theme = 'dark'
    })
    await page.screenshot({
      path: path.join(outDir, 'dashboard-dark.png'),
      fullPage: true,
    })
    await page.evaluate(() => {
      document.documentElement.classList.remove('dark')
      document.documentElement.dataset.theme = 'light'
    })

    // Volumes list
    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()
    await page.screenshot({
      path: path.join(outDir, 'volumes-list.png'),
      fullPage: true,
    })

    // Volume detail (seeded mock volume)
    const first = page.locator('[data-testid="volumes-page"] a[href^="/volumes/"]').first()
    if (await first.count()) {
      await first.click()
      await expect(page.getByTestId('volume-detail-page')).toBeVisible({ timeout: 15_000 })
      await page.screenshot({
        path: path.join(outDir, 'volume-detail.png'),
        fullPage: true,
      })
    }

    // SSO admin UI
    await page.goto('/admin/sso')
    await expect(page.getByTestId('sso-config-page')).toBeVisible()
    await page.screenshot({
      path: path.join(outDir, 'sso-config.png'),
      fullPage: true,
    })

    // Soft assert: screenshots directory should now exist with files
    // (Playwright writes them above; we only verify page still healthy.)
    await expect(page.getByTestId('sso-config-page')).toBeVisible()
  })
})
