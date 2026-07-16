import { expect, test } from '@playwright/test'

/**
 * Phase 1 exit smoke (HIGHLAND_PLAN T1.12):
 * login → create volume → snapshot via manager actions through BFF.
 */
test.describe('Phase 1 parity smoke', () => {
  test('login, create volume, snapshot', async ({ page }) => {
    const volName = `e2e-vol-${Date.now()}`

    await page.goto('/login')
    await expect(page.getByTestId('login-page')).toBeVisible()

    await page.locator('#username').fill('admin')
    await page.locator('#password').fill('highland')
    await page.getByRole('button', { name: /sign in/i }).click()

    await expect(page.getByTestId('app-shell')).toBeVisible()
    await expect(page.getByTestId('dashboard-page')).toBeVisible()

    // Theme toggle works without page errors
    await page.getByTestId('theme-toggle').click()
    await expect
      .poll(async () => page.evaluate(() => document.documentElement.dataset.theme ?? ''))
      .not.toEqual('')

    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()
    // Seeded volume from mock manager
    await expect(page.getByRole('link', { name: 'pvc-db' })).toBeVisible()

    // Create volume
    await page.getByTestId('create-volume').click()
    await page.getByTestId('create-volume-name').fill(volName)
    await page.getByTestId('create-volume-size').fill('1Gi')
    await page.getByTestId('create-volume-submit').click()

    await expect(page.getByRole('link', { name: volName })).toBeVisible({ timeout: 15_000 })

    // Detail + snapshot table UI
    await page.getByRole('link', { name: volName }).click()
    await expect(page.getByTestId('volume-detail-page')).toBeVisible()
    await expect(page.getByRole('heading', { name: volName })).toBeVisible()
    await expect(page.getByTestId('snapshots-panel')).toBeVisible()

    await page.getByTestId('snapshot-create').click()
    await page.getByTestId('snapshot-name').fill('e2e-snap')
    await page.getByTestId('snapshot-create-confirm').click()
    await expect(page.getByText('e2e-snap')).toBeVisible({ timeout: 10_000 })

    // Attach via action form (manager actions map). Volume actions are now
    // consolidated into an "Actions" dropdown menu.
    await page.getByTestId('volume-actions').getByRole('button').click()
    await page.getByRole('menuitem', { name: 'Attach', exact: true }).click()
    const actionDialog = page.getByRole('dialog')
    await expect(actionDialog.getByTestId('action-form-submit')).toBeVisible()
    // host select if present
    const hostSelect = actionDialog.locator('select').first()
    if (await hostSelect.count()) {
      await hostSelect.selectOption({ index: 0 })
    }
    await actionDialog.getByTestId('action-form-submit').click()

    // Events panel present
    await expect(page.getByTestId('volume-events')).toBeVisible()

    // Nodes page still loads via proxy
    await page.goto('/nodes')
    await expect(page.getByTestId('nodes-page')).toBeVisible()
    await expect(page.getByText('node-1')).toBeVisible()
  })

  test('unauthenticated volumes API is rejected by BFF', async ({ request }) => {
    const res = await request.get('/api/v1/lh/volumes')
    expect(res.status()).toBe(401)
  })

  test('CSRF is enforced: an authenticated mutation without the token is rejected', async ({
    page,
  }) => {
    await page.goto('/login')
    await page.locator('#username').fill('admin')
    await page.locator('#password').fill('highland')
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByTestId('app-shell')).toBeVisible()
    // An authenticated GET mints the highland_csrf cookie.
    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()

    // Same-origin POST WITHOUT the X-CSRF-Token header must be rejected (403),
    // even though the session cookie is sent — proving CSRF actually enforces.
    const status = await page.evaluate(async () => {
      const r = await fetch('/api/v1/lh/volumes', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'csrf-should-fail', size: 1073741824 }),
      })
      return r.status
    })
    expect(status).toBe(403)
  })

  test('viewer cannot mutate volumes; admin can run benchmark', async ({ page }) => {
    await page.route('**/api/v1/storage/classes**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [{
            name: 'e2e-storage',
            providerId: 'e2e-csi',
            provisioner: 'e2e.csi.example.com',
            default: true,
          }],
          page: { limit: 500, total: 1 },
        }),
      })
    })
    await page.goto('/login')
    await page.locator('#username').fill('viewer')
    await page.locator('#password').fill('viewer')
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByTestId('app-shell')).toBeVisible()
    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()
    await expect(page.getByTestId('create-volume')).toHaveCount(0)

    await page.evaluate(async () => {
      await fetch('/auth/logout', { method: 'POST', credentials: 'include' })
    })
    await page.goto('/login')
    await page.locator('#username').fill('admin')
    await page.locator('#password').fill('highland')
    await page.getByRole('button', { name: /sign in/i }).click()
    // Wait for the authenticated shell before navigating, otherwise the goto can
    // race the login request and bounce back to /login.
    await expect(page.getByTestId('app-shell')).toBeVisible()
    await page.goto('/benchmarks')
    await expect(page.getByTestId('benchmarks-page')).toBeVisible()
    await page.getByTestId('run-benchmark').click()
    await expect(page.getByText(/bench-|Succeeded|Running|Pending/i).first()).toBeVisible({
      timeout: 15_000,
    })
    await page.goto('/admin/audit')
    await expect(page.getByTestId('audit-page')).toBeVisible()
    await page.goto('/preflight')
    await expect(page.getByTestId('preflight-page')).toBeVisible()
  })

  test('create a v2 (SPDK) data-engine volume with engine-aware frontend gating', async ({ page }) => {
    const volName = `e2e-v2-${Date.now()}`

    await page.goto('/login')
    await page.locator('#username').fill('admin')
    await page.locator('#password').fill('highland')
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByTestId('app-shell')).toBeVisible()

    await page.goto('/volumes')
    await expect(page.getByTestId('volumes-page')).toBeVisible()

    // The mock manager enables `v2-data-engine`, so the engine controls appear.
    await expect(page.getByTestId('volume-filter-engine')).toBeVisible()

    await page.getByTestId('create-volume').click()
    await page.getByTestId('create-volume-name').fill(volName)
    await page.getByTestId('create-volume-size').fill('1Gi')

    // Default engine is v1 → frontend offers iscsi, not nvmf.
    const engine = page.getByTestId('create-volume-data-engine')
    await expect(engine).toBeVisible()
    const frontend = page.getByTestId('create-volume-frontend')
    await expect(frontend.locator('option[value="iscsi"]')).toHaveCount(1)
    await expect(frontend.locator('option[value="nvmf"]')).toHaveCount(0)

    // Switch to v2 → frontend gating flips: nvmf/ublk in, iscsi out.
    await engine.selectOption('v2')
    await expect(frontend.locator('option[value="nvmf"]')).toHaveCount(1)
    await expect(frontend.locator('option[value="ublk"]')).toHaveCount(1)
    await expect(frontend.locator('option[value="iscsi"]')).toHaveCount(0)

    await page.getByTestId('create-volume-submit').click()

    // The new volume row shows a v2 engine badge.
    const row = page.getByRole('row', { name: new RegExp(volName) })
    await expect(row).toBeVisible({ timeout: 15_000 })
    await expect(row.getByText('v2', { exact: true })).toBeVisible()

    // The engine filter narrows to v2.
    await page.getByTestId('volume-filter-engine').selectOption('v2')
    await expect(page.getByRole('link', { name: volName })).toBeVisible()
  })
})
