import { test as base, expect } from '@playwright/test'

// Every browser regression defaults to local-only storage and blocked generation,
// even when a developer's real backend is configured with cloud credentials.
export const test = base.extend<{ isolateCloud: void }>({
  isolateCloud: [async ({ page }, use) => {
    await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user: { id: '99999999-9999-4999-8999-999999999999', email: 'regression@example.test' }, csrfToken: 'test-csrf', expiresAt: '2099-01-01T00:00:00Z' } }))
    await page.route('**/api/auth/login', (route) => route.abort())
    await page.route('**/api/auth/logout', (route) => route.abort())
    await page.route('**/api/credits', (route) => route.fulfill({ json: { available: 200, reserved: 0 } }))
    await page.route('**/api/credits/quote', (route) => {
      const input = route.request().postDataJSON()
      return route.fulfill({ json: { points: input.kind === 'video' ? 82 : input.model?.includes('pro') ? 16 : 6, priceVersion: 'test-v1' } })
    })
    await page.route('**/api/credits/ledger?*', (route) => route.fulfill({ json: { entries: [], hasMore: false } }))
    await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: false } }))
    await page.route('**/api/canvases**', (route) => route.abort())
    await page.route('**/api/tasks/**', (route) => route.abort())
    await page.route('**/api/storage/config', (route) => route.fulfill({ json: { enabled: false, maxImageBytes: 20 << 20, maxVideoBytes: 200 << 20, maxAudioBytes: 15_000_000 } }))
    await page.route('**/api/assets/content?*', (route) => route.abort())
    await page.route('**/api/assets?*', (route) => route.abort())
    await page.route('**/api/images/generations', (route) => route.abort())
    await page.route('**/api/videos/generations', (route) => route.abort())
    await use()
  }, { auto: true }],
})
export { expect }
