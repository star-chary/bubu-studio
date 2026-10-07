import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { expect, test } from './setup'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'

const canvasId = '33333333-3333-4333-8333-333333333333'
const sourceId = '11111111-1111-4111-8111-111111111111'
const imageId = '22222222-2222-4222-8222-222222222222'
const videoId = '44444444-4444-4444-8444-444444444444'
const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))

async function mockCreditsCanvas(page: Page, initialCredits: number) {
  let available = initialCredits
  let reserved = 0
  let generationCalls = 0
  const submitted: Record<string, unknown>[] = []
  const tasks: GenerationTask[] = []
  const canvas: SavedCanvas = {
    id: canvasId, title: '积分测试画布', version: 1, updatedAt: new Date().toISOString(), tasks, results: [],
    assets: [{ key: 'reference.png', url: '/api/assets/content?key=reference.png', canvasId, nodeId: sourceId,
      kind: 'images', source: 'uploads', contentType: 'image/png', bytes: imageBytes.length,
      media: { width: 960, height: 540 }, videoReference: { eligible: true } }],
    snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: sourceId, position: { x: 20, y: 145 }, data: { kind: 'image', name: '参考图片', origin: 'upload' } },
      { id: imageId, position: { x: 400, y: 145 }, data: { kind: 'image', name: '图片生成节点', prompt: '' } },
      { id: videoId, position: { x: 820, y: 145 }, data: { kind: 'video', name: '视频生成节点', prompt: '', referenceIds: [sourceId] } },
    ] },
  }
  await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/credits', route => route.fulfill({ json: { available, reserved } }))
  await page.route('**/api/credits/quote', route => {
    const body = route.request().postDataJSON()
    const points = body.kind === 'video' ? 82 + (Number(body.duration) - 4) * 20 + (body.resolution === '720p' ? 100 : 0) : 6
    return route.fulfill({ json: { points, priceVersion: 'test-v1' } })
  })
  await page.route('**/api/credits/ledger?*', route => route.fulfill({ json: { entries: [{
    id: 1, operation: 'grant', availableDelta: initialCredits, reservedDelta: 0,
    availableAfter: initialCredits, reservedAfter: 0, createdAt: new Date().toISOString(),
  }], hasMore: false } }))
  await page.route('**/api/assets/content?*', route => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/canvases**', route => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      canvas.snapshot = body.snapshot
      canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  for (const kind of ['images', 'videos']) {
    await page.route(`**/api/${kind}/generations`, route => {
      generationCalls++
      const body = route.request().postDataJSON() as Record<string, unknown>
      submitted.push(body)
      const { taskId, acceptedPoints, priceVersion, ...input } = body
      available -= Number(acceptedPoints)
      reserved += Number(acceptedPoints)
      const now = new Date().toISOString()
      const task: GenerationTask = { id: String(taskId), kind: kind === 'images' ? 'image' : 'video', input: input as unknown as GenerationTask['input'], status: 'queued', createdAt: now, updatedAt: now }
      tasks.push(task)
      return route.fulfill({ status: 202, json: task })
    })
  }
  await page.goto(`/?canvas=${canvasId}`)
  await expect(page.getByRole('article', { name: '参考图片', exact: true })).toContainText('960 × 540')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  return { calls: () => generationCalls, submitted, canvas }
}

test('无积分时图片和视频显示服务端报价，按钮禁用且不发送生成请求', async ({ page }) => {
  const db = await mockCreditsCanvas(page, 0)
  await page.getByRole('article', { name: '图片生成节点', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill('窗边的云')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('6')
  await expect(page.getByRole('status').filter({ hasText: '积分不足' })).toBeVisible()
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  await page.getByRole('article', { name: '视频生成节点', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '视频提示词' }).fill('镜头向前移动')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('82')
  await expect(page.getByRole('status').filter({ hasText: '积分不足' })).toBeVisible()
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  expect(db.calls()).toBe(0)
})

test('图片提交携带用户已见报价，冻结后刷新余额并可查看流水', async ({ page }) => {
  const db = await mockCreditsCanvas(page, 6)
  await page.getByRole('article', { name: '图片生成节点', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill('窗边的云')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('6')
  await page.locator('.credit-cost-trigger').click()
  await expect(page.locator('.credit-cost-popover')).toContainText('本次需要 6 积分 · 可用 6 · 冻结 0')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(db.calls).toBe(1)
  expect(db.submitted[0]).toMatchObject({ acceptedPoints: 6, priceVersion: 'test-v1', canvasId, nodeId: imageId, prompt: '窗边的云' })
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toContainText('积分 0')
  await page.getByRole('button', { name: '查看积分余额和流水' }).click()
  await expect(page.getByRole('region', { name: '积分账户' })).toContainText('测试积分发放')
})

test('视频参数变化使旧报价失效，新报价超出余额时无法提交', async ({ page }) => {
  const db = await mockCreditsCanvas(page, 200)
  await page.getByRole('article', { name: '视频生成节点', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '视频提示词' }).fill('镜头向前移动')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('82')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: /^视频参数，/ }).click()
  await page.getByRole('spinbutton', { name: '视频时长秒数' }).fill('12')
  await page.keyboard.press('Escape')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('242')
  await expect(page.getByRole('status').filter({ hasText: '积分不足' })).toBeVisible()
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  expect(db.calls()).toBe(0)
})

test('窄屏可查看积分流水，视频生成栏不重叠', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockCreditsCanvas(page, 200)
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toBeVisible()
  await page.getByRole('button', { name: '查看积分余额和流水' }).click()
  await expect(page.getByRole('region', { name: '积分账户' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: '关闭积分记录' }).click()
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.getByRole('article', { name: '视频生成节点', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '视频提示词' }).fill('镜头向前移动')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('82')
  await page.setViewportSize({ width: 390, height: 844 })
  const bounds = await page.locator('.node-prompt-panel').evaluate(panel => {
    const footer = panel.querySelector('.prompt-generation-footer')!.getBoundingClientRect()
    const actions = panel.querySelector('.generation-actions')!.getBoundingClientRect()
    const controls = panel.querySelector('.video-generation-controls')!.getBoundingClientRect()
    return { footerRight: footer.right, actionsLeft: actions.left, actionsRight: actions.right, controlsRight: controls.right }
  })
  expect(bounds.actionsRight).toBeLessThanOrEqual(bounds.footerRight)
  expect(bounds.controlsRight).toBeLessThanOrEqual(bounds.actionsLeft)
})

test('临时画布即使有积分也不能绕过任务冻结直接生成', async ({ page }) => {
  let calls = 0
  page.on('request', request => { if (request.url().endsWith('/api/images/generations')) calls++ })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await page.mouse.click(220, 160, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill('一片海')
  await expect(page.getByRole('status').filter({ hasText: '请先使用已保存画布' })).toBeVisible()
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  expect(calls).toBe(0)
})

test('新建节点立即输入时先保存节点，再向后端获取报价', async ({ page }) => {
  const db = await mockCreditsCanvas(page, 200)
  let sawSavedNode = false
  await page.route('**/api/credits/quote', route => {
    const body = route.request().postDataJSON()
    sawSavedNode = db.canvas.snapshot.nodes.some(node => node.id === body.nodeId)
    return route.fulfill({ status: sawSavedNode ? 200 : 404, json: sawSavedNode ? { points: 6, priceVersion: 'test-v1' } : { error: { message: '节点未保存' } } })
  })
  await page.mouse.click(690, 550, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill('初次输入')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('6')
  expect(sawSavedNode).toBe(true)
})
