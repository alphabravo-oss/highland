import { expect, test, type Page } from '@playwright/test'

async function login(page: Page, username: string, password: string) {
  await page.goto('/login')
  await page.locator('#username').fill(username)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

test.describe('administration navigation', () => {
  test('gives admins one sidebar entry backed by an administration hub', async ({ page }) => {
    await login(page, 'admin', 'highland')
    const sidebar = page.getByTestId('sidebar')

    await expect(sidebar.getByRole('link', { name: 'Administration', exact: true })).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'Users', exact: true })).toHaveCount(0)
    await expect(sidebar.getByRole('link', { name: 'Enterprise SSO', exact: true })).toHaveCount(0)
    await expect(sidebar.getByRole('link', { name: 'Audit Log', exact: true })).toHaveCount(0)

    await sidebar.getByRole('link', { name: 'Administration', exact: true }).click()
    await expect(page.getByTestId('admin-page')).toBeVisible()
    const main = page.getByRole('main')
    await expect(main.getByRole('link', { name: /Users/ })).toBeVisible()
    await expect(main.getByRole('link', { name: /Enterprise SSO/ })).toBeVisible()
    await expect(main.getByRole('link', { name: /Audit Log/ })).toBeVisible()

    await main.getByRole('link', { name: /Users/ }).click()
    await expect(page).toHaveURL(/\/admin\/users$/)
    await expect(page.getByTestId('admin-users-page')).toBeVisible()
  })

  test('does not show the administration entry to an operator', async ({ page }) => {
    await login(page, 'operator', 'operator')
    await expect(page.getByTestId('sidebar').getByRole('link', { name: 'Administration', exact: true })).toHaveCount(0)

    await page.goto('/admin')
    await expect(page.getByTestId('admin-page')).toBeVisible()
    await expect(page.getByText('Admins only')).toBeVisible()
  })
})
