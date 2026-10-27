import { test as base, expect, type Page } from '@playwright/test'

// Every browser regression defaults to local-only storage and blocked generation,
// even when a developer's real backend is configured with cloud credentials.
// 模型及基础参数来自本地静态表，旧目录接口不应再被访问。
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
    await page.route('**/api/model-catalog', (route) => route.abort())
    await use()
  }, { auto: true }],
})

// 新节点默认未选择模型；历史 Ark 生成用例需要在生成前显式选择 Ark 模型。
export async function selectArkModel(page: Page, kind: 'image' | 'video', modelName: string) {
  await page.getByRole('button', { name: kind === 'image' ? /^选择模型，/ : /^选择视频模型，/ }).click()
  const listbox = page.getByRole('listbox', { name: kind === 'image' ? '图片模型' : '视频模型' })
  await listbox.getByRole('option', { name: modelName, exact: true }).click()
  await expect(listbox).toHaveCount(0)
}

export async function addNodeViaContextMenu(page: Page, x: number, y: number, kind: '图片' | '视频') {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
}

export { expect }
