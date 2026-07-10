import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

async function expectNoSeriousA11y(page: Page, label: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  const serious = results.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical',
  )
  expect(serious, `${label}: ${JSON.stringify(serious, null, 2)}`).toEqual([])
}

async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await expect(page.getByTestId('login-page')).toBeVisible()
  await page.locator('#username').fill('admin')
  await page.locator('#password').fill('highland')
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

test.describe('a11y (axe WCAG 2.1 AA)', () => {
  test('login page has no serious violations', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByTestId('login-page')).toBeVisible()
    await expectNoSeriousA11y(page, 'login')
  })

  test('dashboard after login has no serious violations', async ({ page }) => {
    await loginAsAdmin(page)
    await expect(page.getByTestId('dashboard-page')).toBeVisible()
    await expectNoSeriousA11y(page, 'dashboard')
  })

  test('volumes page has no serious violations', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()
    await expectNoSeriousA11y(page, 'volumes')
  })

  test('SSO config page has no serious violations', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/sso')
    await expect(page.getByTestId('sso-config-page')).toBeVisible()
    // Wait until config load finishes — disabled primary buttons use opacity-50 and fail contrast.
    await expect(page.getByTestId('sso-save')).toBeEnabled()
    await expect(page.getByTestId('sso-enabled')).toBeVisible()
    await expectNoSeriousA11y(page, 'sso-config')
  })
})

