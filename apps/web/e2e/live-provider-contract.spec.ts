import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

const live = process.env.HIGHLAND_LIVE_E2E === '1'
const username = process.env.HIGHLAND_E2E_USERNAME ?? ''
const password = process.env.HIGHLAND_E2E_PASSWORD ?? ''

type Provider = {
  id: string
  supportLevel: string
  capabilities: string[]
  resourceKinds?: string[]
}

const inventoryCapabilityPaths: Record<string, string> = {
  'inventory.claims.read': '/api/v1/storage/claims',
  'inventory.volumes.read': '/api/v1/storage/volumes',
  'inventory.attachments.read': '/api/v1/storage/attachments',
  'inventory.snapshots.read': '/api/v1/storage/snapshots',
  'inventory.capacity.read': '/api/v1/storage/capacity',
  'inventory.events.read': '/api/v1/storage/events',
}

async function login(page: Page) {
  await page.goto('/login')
  await page.locator('#username').fill(username)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByTestId('app-shell')).toBeVisible()
}

async function expectOK(request: APIRequestContext, path: string) {
  const response = await request.get(path)
  expect(response.status(), `${path}: ${await response.text()}`).toBe(200)
  return response.json() as Promise<unknown>
}

test.describe('live provider integration contract', () => {
  test.skip(!live || !username || !password, 'live cluster credentials are required')

  test('every advertised provider surface is backed by a working API', async ({ page }) => {
    test.setTimeout(10 * 60_000)
    await login(page)
    const listing = await expectOK(page.request, '/api/v1/storage/providers') as { data: Provider[] }
    expect(listing.data.length).toBeGreaterThanOrEqual(4)

    for (const provider of listing.data) {
      await test.step(provider.id, async () => {
        const descriptor = await expectOK(page.request, `/api/v1/storage/providers/${encodeURIComponent(provider.id)}`) as Provider
        expect(descriptor.capabilities.length, `${provider.id} has no capabilities`).toBeGreaterThan(0)
        for (const capability of descriptor.capabilities) {
          const inventoryPath = inventoryCapabilityPaths[capability]
          if (inventoryPath) {
            await expectOK(page.request, `${inventoryPath}?provider=${encodeURIComponent(provider.id)}&limit=2`)
          }
        }
        if (descriptor.capabilities.includes('provider.health.read')) {
          await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/health`)
        }

        if (provider.supportLevel === 'managed') {
          await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/summary`)
          const relationshipKind = descriptor.resourceKinds?.[0]
          if (relationshipKind) {
            await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/relationships?kind=${encodeURIComponent(relationshipKind)}`)
          }
          await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/drift`)
          await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/capacity/forecast?measure=pvc-requested&horizon=720h`)
        }

        for (const kind of descriptor.resourceKinds ?? []) {
          const path = `/api/v1/providers/${encodeURIComponent(provider.id)}/resources/${encodeURIComponent(kind)}?limit=2`
          const body = await expectOK(page.request, path) as { data: unknown[]; page: { total: number } }
          expect(Array.isArray(body.data), `${provider.id}/${kind} did not return an array`).toBe(true)
          expect(typeof body.page?.total, `${provider.id}/${kind} omitted pagination metadata`).toBe('number')
          const first = body.data[0] as Record<string, unknown> | undefined
          const id = first?.id ?? first?.name
          if (id) {
            await expectOK(page.request, `/api/v1/providers/${encodeURIComponent(provider.id)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(String(id))}`)
          }
        }
      })
    }
  })
})
