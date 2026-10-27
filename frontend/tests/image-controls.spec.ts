import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'

const canvasID = '11111111-1111-4111-8111-111111111111'
const targetID = '33333333-3333-4333-8333-333333333333'
const otherID = '44444444-4444-4444-8444-444444444444'
const parameters = (page: Page) => page.getByRole('button', { name: /^图片参数，/ })
const popup = (page: Page) => page.getByRole('dialog', { name: '图片参数', exact: true })
const panel = (page: Page) => page.getByRole('dialog', { name: '图片提示词', exact: true })
const lite = 'doubao-seedream-5-0-260128'
const pro = 'doubao-seedream-5-0-pro-260628'

async function setup(page: Page, available = 200) {
  const tasks: GenerationTask[] = []
  const canvas: SavedCanvas = { id: canvasID, title: '图片参数测试', version: 1, assets: [], tasks, results: [], updatedAt: new Date().toISOString(), snapshot: {
    viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: targetID, position: { x: 450, y: 140 }, data: { kind: 'image', name: '图片 A', prompt: '保留当前提示词' } },
      { id: otherID, position: { x: 970, y: 140 }, data: { kind: 'image', name: '图片 B', prompt: '独立草稿' } },
    ],
  } }
  const requests: Record<string, any>[] = []
  const quotes: Record<string, any>[] = []
  let failAt = 0
  await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/credits', route => route.fulfill({ json: { available, reserved: 0 } }))
  await page.route('**/api/credits/quote', route => {
    const body = route.request().postDataJSON(); quotes.push(body)
    const points = body.model === pro ? body.imageOptions?.resolution === '2K' ? 16 : 8 : 6
    return route.fulfill({ json: { points, priceVersion: 'test-v1' } })
  })
  await page.route('**/api/canvases**', route => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      expect(body.version).toBe(canvas.version)
      canvas.snapshot = body.snapshot; canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.route('**/api/images/generations', route => {
    const body = route.request().postDataJSON(); requests.push(body)
    if (failAt === requests.length) return route.fulfill({ status: 409, json: { error: { code: 'INSUFFICIENT_CREDITS', message: '可用积分不足，本张未提交。' } } })
    expect(canvas.snapshot.nodes.some(n => n.id === body.nodeId)).toBe(true)
    const { taskId, acceptedPoints, priceVersion, ...input } = body
    const task: GenerationTask = { id: taskId, input, kind: 'image', status: 'queued', creditPoints: acceptedPoints, creditPriceVersion: priceVersion, creditStatus: 'reserved', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }
    tasks.push(task)
    return route.fulfill({ status: 202, json: task })
  })
  const bytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
  await page.route('**/mock-result/*.png', route => route.fulfill({ contentType: 'image/png', body: bytes }))
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '图片 A', exact: true }).click({ position: { x: 60, y: 70 } })
  return { canvas, requests, quotes, tasks, failSubmission: (at: number) => { failAt = at } }
}

test('按模型显示分辨率，参数按节点保存，旧画布默认值及刷新恢复', async ({ page }, info) => {
  const db = await setup(page)
  await expect(parameters(page)).toHaveText('自动 · 2K · 1张')
  await parameters(page).click()
  await expect(popup(page).getByRole('group', { name: '图片分辨率' }).getByRole('radio')).toHaveCount(3)
  await expect(page.getByRole('radio', { name: '1K', exact: true })).toHaveCount(0)
  await page.getByRole('radio', { name: '4K', exact: true }).check()
  await page.getByRole('radio', { name: '16:9', exact: true }).check()
  await page.getByRole('radio', { name: '4张' }).check()
  await expect(parameters(page)).toHaveText('16:9 · 4K · 4张')
  await page.keyboard.press('Escape')
  await expect(panel(page)).toBeVisible()
  await expect(parameters(page)).toBeFocused()
  await page.getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: 'Doubao-Seedream-5.0-pro', exact: true }).click()
  await expect(parameters(page)).toHaveText('16:9 · 2K · 4张')
  await parameters(page).click()
  await expect(page.getByRole('radio', { name: '4K', exact: true })).toHaveCount(0)
  await page.getByRole('radio', { name: '1.5K', exact: true }).check()
  const promptBox = (await panel(page).boundingBox())!
  const popupBox = (await popup(page).boundingBox())!
  await page.screenshot({ path: info.outputPath('image-parameters-pro.png'), clip: {
    x: promptBox.x - 12, y: popupBox.y - 12, width: promptBox.width + 24,
    height: promptBox.y + promptBox.height - popupBox.y + 24,
  } })
  await page.keyboard.press('Escape')
  await expect.poll(() => db.canvas.snapshot.nodes[0]!.data.imageOptions).toEqual({ resolution: '1.5K', ratio: '16:9', count: 4 })
  await page.getByRole('article', { name: '图片 B', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(parameters(page)).toHaveText('自动 · 2K · 1张')
  await page.reload()
  await page.getByRole('article', { name: '图片 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(parameters(page)).toHaveText('16:9 · 1.5K · 4张')
  await expect(page.getByRole('button', { name: /^选择模型/ })).toContainText('pro')
  expect(db.requests).toHaveLength(0)
})

test('四张显示总积分并创建四个独立任务，结果各自回填且刷新可恢复', async ({ page }) => {
  const db = await setup(page)
  await parameters(page).click()
  await page.getByRole('radio', { name: '3K', exact: true }).check()
  await page.getByRole('radio', { name: '9:16', exact: true }).check()
  await page.getByRole('radio', { name: '4张' }).check()
  await page.keyboard.press('Escape')
  await expect(page.getByLabel(/^查看本次积分明细/)).toContainText('24')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.requests.length).toBe(4)
  expect(new Set(db.requests.map(r => r.taskId)).size).toBe(4)
  expect(new Set(db.requests.map(r => r.nodeId)).size).toBe(4)
  expect(db.requests[0]!.nodeId).toBe(targetID)
  for (const request of db.requests) {
    expect(request).toMatchObject({ prompt: '保留当前提示词', model: lite, canvasId: canvasID, imageOptions: { resolution: '3K', ratio: '9:16' }, acceptedPoints: 6, priceVersion: 'test-v1' })
    expect(request.imageOptions).not.toHaveProperty('count')
  }
  expect(db.canvas.snapshot.nodes).toHaveLength(5)
  expect(db.canvas.snapshot.nodes[1]!.data.prompt).toBe('独立草稿')
  for (const node of db.canvas.snapshot.nodes.slice(2)) expect(node.data.imageOptions?.count).toBe(1)
  await page.keyboard.press('Escape')
  for (const [index, task] of db.tasks.entries()) {
    task.status = 'succeeded'; task.creditStatus = 'settled'; task.updatedAt = new Date(Date.now() + 1000).toISOString()
    task.result = { model: lite, url: `/mock-result/${index}.png` }
  }
  db.canvas.results = [...db.tasks]
  await expect(page.locator('.vue-flow__node img')).toHaveCount(4)
  await page.reload()
  await expect(page.locator('.vue-flow__node img')).toHaveCount(4)
  expect(db.requests).toHaveLength(4)
})

test('总积分不足时禁用批量生成，减少数量后可提交', async ({ page }) => {
  const db = await setup(page, 20)
  await parameters(page).click()
  await page.getByRole('radio', { name: '4张' }).check()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  await expect(panel(page).getByRole('status')).toHaveText('积分不足')
  await parameters(page).click()
  await page.getByRole('radio', { name: '2张' }).check()
  await page.keyboard.press('Escape')
  await expect(page.getByLabel(/^查看本次积分明细/)).toContainText('12')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.requests.length).toBe(2)
})

test('Pro 批量使用低分辨率报价，提交中修改草稿不改变本批快照', async ({ page }) => {
  const db = await setup(page)
  await page.getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: 'Doubao-Seedream-5.0-pro', exact: true }).click()
  await parameters(page).click()
  await page.getByRole('radio', { name: '1K', exact: true }).check()
  await page.getByRole('radio', { name: '16:9', exact: true }).check()
  await page.getByRole('radio', { name: '4张' }).check()
  await page.keyboard.press('Escape')
  await expect(page.getByLabel(/^查看本次积分明细/)).toContainText('32')
  let release!: () => void
  let held = false
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route('**/api/images/generations', async route => {
    if (!held) { held = true; await pending }
    await route.fallback()
  })
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => held).toBe(true)
  await page.getByRole('textbox', { name: '图片提示词', exact: true }).fill('下一批的草稿')
  await parameters(page).click()
  await page.getByRole('radio', { name: '2K', exact: true }).check()
  await page.getByRole('radio', { name: '1张' }).check()
  await page.keyboard.press('Escape')
  release()
  await expect.poll(() => db.requests.length).toBe(4)
  for (const request of db.requests) expect(request).toMatchObject({ model: pro, prompt: '保留当前提示词', imageOptions: { resolution: '1K', ratio: '16:9' }, acceptedPoints: 8 })
  await expect(page.getByRole('textbox', { name: '图片提示词', exact: true })).toHaveText('下一批的草稿')
})

test('部分提交失败保留已接收任务，并停止后续提交', async ({ page }) => {
  const db = await setup(page)
  db.failSubmission(2)
  await parameters(page).click()
  await page.getByRole('radio', { name: '4张' }).check()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(page.getByText(/本批共 4 张，已确认提交 1 张/)).toBeVisible()
  expect(db.requests).toHaveLength(2)
  expect(db.tasks).toHaveLength(1)
  expect(db.canvas.snapshot.nodes).toHaveLength(5)
  await page.mouse.click(150, 800)
  await expect(panel(page)).toHaveCount(0)
  const pending = db.canvas.snapshot.nodes.find(node => node.data.name === '图片 A · 3')!
  await page.getByRole('article', { name: pending.data.name, exact: true }).click({ position: { x: 40, y: 60 } })
  await expect(panel(page).getByRole('alert')).toHaveText('本批提交已停止，此图片尚未提交，可单独生成。')
  await expect(parameters(page)).toHaveText('自动 · 2K · 1张')
})

test('键盘与窄屏交互，弹层不越界、不缩放画布，模型菜单互斥', async ({ page }, info) => {
  await setup(page)
  await page.setViewportSize({ width: 375, height: 812 })
  await parameters(page).focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('radio', { name: '2K', exact: true })).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('radio', { name: '3K', exact: true })).toBeChecked()
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')
  await popup(page).hover()
  await page.mouse.wheel(0, 180)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
  for (const size of [{ width: 375, height: 812 }, { width: 320, height: 480 }]) {
    await page.setViewportSize(size)
    await expect.poll(async () => {
      const box = await popup(page).boundingBox()
      return !!box && box.x >= 16 && box.y >= 16 && box.x + box.width <= size.width - 16 && box.y + box.height <= size.height - 16
    }).toBe(true)
  }
  await page.screenshot({ path: info.outputPath('image-parameters-mobile.png') })
  // The node footer can be below a 320×480 viewport; the popup boundary was
  // checked above, while the remaining menu and focus checks need a visible footer.
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.keyboard.press('Escape')
  await expect(parameters(page)).toBeFocused()
  await page.getByRole('button', { name: /^选择模型/ }).click()
  await expect(page.getByRole('listbox', { name: '图片模型' })).toBeVisible()
  await parameters(page).click()
  await expect(page.getByRole('listbox', { name: '图片模型' })).toHaveCount(0)
  await expect(popup(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '放大输入框', exact: true }).click()
  await parameters(page).click()
  await page.getByRole('button', { name: '恢复输入框大小', exact: true }).click()
  await expect(popup(page)).toHaveCount(0)
  await expect(panel(page)).toBeVisible()
})
