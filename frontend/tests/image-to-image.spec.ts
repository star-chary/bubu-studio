import type { Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { test, expect } from './setup'

const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const panel = (page: Page) => page.getByRole('dialog')
function makeAsset(canvasId: string, nodeId: string, source = 'uploads') {
  const key = `frame-space/dev/canvases/${canvasId}/${source}/images/${nodeId}/e753b4e4-2fe1-41e1-ab3b-f3e6e57c4b82.png`
  return { key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, kind: 'images', source, canvasId, nodeId, contentType: 'image/png', bytes: imageBytes.length }
}
async function addTarget(page: Page) {
  await page.mouse.click(810, 140, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const target = page.getByRole('article', { name: '图片节点 1', exact: true })
  await target.click({ position: { x: 65, y: 75 } })
  await panel(page).getByRole('textbox').fill('保留构图，把颜色变成蓝色')
  return target
}
async function uploadReferences(page: Page, count: number) {
  await page.mouse.click(50, 140, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(Array.from({ length: count }, (_, i) => ({ name: `reference-${i + 1}.png`, mimeType: 'image/png', buffer: imageBytes })))
  await expect(page.getByRole('article', { name: 'reference-1.png', exact: true })).toContainText('960 × 540')
}
async function useSavedCanvas(page: Page) {
  const id = '33333333-3333-4333-8333-333333333333'
  const canvas = { id, title: '图生图测试', version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [],
    snapshot: { nodes: [] as unknown[], viewport: { x: 0, y: 0, zoom: 1 } } }
  await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', route => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot
      canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
}
test.beforeEach(async ({ page }) => {
  await page.route('**/api/storage/config', route => route.fulfill({ json: { enabled: true, maxImageBytes: 20 << 20, maxVideoBytes: 200 << 20 } }))
  await page.route('**/api/assets/content?*', route => route.fulfill({ contentType: 'image/png', body: imageBytes }))
})

test('参考图保存中禁止生成，保存完成后可生成单张图片', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  let generationCalls = 0
  page.on('request', request => { if (request.url().endsWith('/api/images/generations')) generationCalls++ })
  await page.route('**/api/assets?*', async route => {
    const q = new URL(route.request().url()).searchParams
    await pending
    await route.fulfill({ status: 201, json: makeAsset(q.get('canvasId')!, q.get('nodeId')!) })
  })
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await uploadReferences(page, 1)
  await addTarget(page)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await page.getByRole('article', { name: 'reference-1.png', exact: true }).click({ position: { x: 65, y: 75 } })
  await page.keyboard.press('Escape')
  await expect(panel(page)).toContainText('参考图片正在保存')
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  release()
  await expect(panel(page)).toContainText('将使用以上 1 张参考图片，生成单张图片')
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  expect(generationCalls).toBe(0)
})

test('多参考图按选择顺序提交，等待中修改参考和草稿只影响下次生成', async ({ page }, testInfo) => {
  const assets: ReturnType<typeof makeAsset>[] = []
  await page.route('**/api/assets?*', route => {
    const q = new URL(route.request().url()).searchParams
    const asset = makeAsset(q.get('canvasId')!, q.get('nodeId')!)
    assets.push(asset)
    return route.fulfill({ status: 201, json: asset })
  })
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  let calls = 0
  let submitted: any
  await page.route('**/api/images/generations', async route => {
    calls++; submitted = route.request().postDataJSON()
    expect(route.request().headers().authorization).toBeUndefined()
    await pending
    const asset = makeAsset(submitted.canvasId, submitted.nodeId, 'generated')
    const { taskId, acceptedPoints, priceVersion, ...input } = submitted
    const now = new Date().toISOString()
    await route.fulfill({ status: 202, json: { id: taskId, kind: 'image', input, status: 'succeeded',
      result: { url: asset.url, model: submitted.model, asset }, creditPoints: acceptedPoints, creditPriceVersion: priceVersion,
      creditStatus: 'settled', createdAt: now, updatedAt: now } })
  })
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await uploadReferences(page, 2)
  // Move the second reference down so the target has its own visible space.
  const source2 = page.getByRole('article', { name: 'reference-2.png', exact: true })
  const box = (await source2.boundingBox())!
  await page.mouse.move(box.x + 50, box.y + 80); await page.mouse.down()
  await page.mouse.move(100, 550, { steps: 12 }); await page.mouse.up()
  await page.keyboard.press('Escape')
  const target = await addTarget(page)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await source2.click({ position: { x: 65, y: 75 } })
  await page.getByRole('article', { name: 'reference-1.png', exact: true }).click({ position: { x: 65, y: 75 } })
  await page.keyboard.press('Escape')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  await panel(page).getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => calls).toBe(1)
  const source1Id = await page.getByRole('article', { name: 'reference-1.png', exact: true }).locator('..').getAttribute('data-id')
  const source2Id = await source2.locator('..').getAttribute('data-id')
  expect(submitted.referenceKeys).toEqual([assets.find(a => a.nodeId === source2Id)!.key, assets.find(a => a.nodeId === source1Id)!.key])
  expect(submitted).not.toHaveProperty('image')
  await panel(page).getByRole('button', { name: '移除参考 reference-2.png', exact: true }).click()
  await panel(page).getByRole('textbox').fill('下一次的草稿')
  release()
  await expect(target.getByText('已保存', { exact: true })).toHaveCount(0)
  await expect(target).toContainText('960 × 540')
  await expect(page.getByRole('article')).toHaveCount(3)
  await expect(panel(page).getByRole('textbox')).toHaveText('下一次的草稿')
  await expect(panel(page).getByRole('listitem')).toHaveCount(1)
  expect(calls).toBe(1)
  await page.screenshot({ path: testInfo.outputPath('image-to-image-single-output.png') })
})

test('参考图保存失败时说明原因，移除参考可继续文生图', async ({ page }) => {
  await page.route('**/api/assets?*', route => route.fulfill({ status: 502, json: { error: { message: '保存失败' } } }))
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await uploadReferences(page, 1)
  await expect(page.getByRole('article', { name: 'reference-1.png', exact: true }).getByRole('alert')).toContainText('保存失败')
  await addTarget(page)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await page.getByRole('article', { name: 'reference-1.png', exact: true }).click({ position: { x: 65, y: 75 } })
  await page.keyboard.press('Escape')
  await expect(panel(page)).toContainText('参考图片保存失败')
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  await panel(page).getByRole('button', { name: '移除参考 reference-1.png', exact: true }).click()
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
})

test('切换模型重新检查参考数量，超出 Pro 上限时阻止提交', async ({ page }) => {
  await page.route('**/api/assets?*', route => {
    const q = new URL(route.request().url()).searchParams
    return route.fulfill({ status: 201, json: makeAsset(q.get('canvasId')!, q.get('nodeId')!) })
  })
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  const zoomOut = page.getByRole('button', { name: '缩小画布', exact: true })
  for (let i = 0; i < 12 && await zoomOut.isEnabled(); i++) await zoomOut.click()
  await expect(zoomOut).toBeDisabled()
  for (let i = 0; i < 11; i++) {
    await page.mouse.click(40 + (i % 3) * 165, 110 + Math.floor(i / 3) * 145, { button: 'right' })
    const chooser = page.waitForEvent('filechooser')
    await page.getByRole('menuitem', { name: '上传', exact: true }).click()
    await (await chooser).setFiles({ name: `limit-${i}.png`, mimeType: 'image/png', buffer: imageBytes })
  }
  await page.mouse.click(900, 140, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 30, y: 30 } })
  await panel(page).getByRole('textbox').fill('融合参考图的颜色')
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  for (let i = 0; i < 11; i++) await page.getByRole('article', { name: `limit-${i}.png`, exact: true }).click({ position: { x: 30, y: 30 } })
  await page.keyboard.press('Escape')
  const generate = panel(page).getByRole('button', { name: '生成图片', exact: true })
  await expect(generate).toBeEnabled()
  await panel(page).getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: 'Doubao-Seedream-5.0-pro', exact: true }).click()
  await expect(panel(page)).toContainText('所选模型最多支持 10 张参考图片')
  await expect(generate).toBeDisabled()
  await panel(page).getByRole('button', { name: '移除参考 limit-0.png', exact: true }).click()
  await expect(generate).toBeEnabled()
})
