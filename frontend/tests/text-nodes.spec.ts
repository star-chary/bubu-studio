import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { expect, selectArkModel, test } from './setup'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'

const canvasId = '33333333-3333-4333-8333-333333333333'
const textA = '11111111-1111-4111-8111-111111111111'
const videoId = '22222222-2222-4222-8222-222222222222'
const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))

async function setupCanvas(page: Page, initial?: SavedCanvas) {
  const canvas: SavedCanvas = initial ?? { id: canvasId, title: '文本测试', version: 0, updatedAt: new Date().toISOString(),
    snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } }, assets: [], tasks: [], results: [] }
  const submitted: GenerationTask[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', (route) => {
    const request = route.request()
    if (new URL(request.url()).pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks: submitted, hasMore: false } })
    if (request.method() === 'PUT') {
      const body = request.postDataJSON()
      if (body.version !== canvas.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '版本冲突' } } })
      canvas.snapshot = body.snapshot
      canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: { ...canvas, tasks: submitted } })
  })
  for (const kind of ['images', 'videos']) {
    await page.route(`**/api/${kind}/generations`, (route) => {
      const { taskId, ...input } = route.request().postDataJSON()
      const task: GenerationTask = { id: taskId, kind: kind === 'images' ? 'image' : 'video', input,
        status: 'running', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }
      submitted.push(task)
      return route.fulfill({ status: 202, json: task })
    })
  }
  await page.goto(`/?canvas=${canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  return { canvas, submitted, errors }
}

async function addNode(page: Page, x: number, y: number, kind: '文本' | '图片') {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
}

test('文本按连接顺序进入图片任务，保存恢复并冻结已提交内容', async ({ page }, testInfo) => {
  const db = await setupCanvas(page)
  await addNode(page, 50, 110, '文本')
  await page.getByRole('textbox', { name: '文本节点 1内容' }).fill('海边')
  await addNode(page, 50, 480, '文本')
  await page.getByRole('textbox', { name: '文本节点 2内容' }).fill('水彩')
  await addNode(page, 600, 110, '图片')
  const target = page.getByRole('article', { name: '图片节点 3', exact: true })
  await target.click({ position: { x: 60, y: 60 } })
  await selectArkModel(page, 'image', 'Doubao-Seedream-5.0-lite')
  await page.getByRole('textbox', { name: '图片提示词' }).fill('一只小猫')
  await page.getByRole('button', { name: '文本', exact: true }).click()
  await page.getByRole('article', { name: '文本节点 2', exact: true }).locator('.selection-overlay').click()
  await page.getByRole('article', { name: '文本节点 1', exact: true }).locator('.selection-overlay').click()
  await page.getByRole('button', { name: '返回节点' }).click()
  await expect(page.getByLabel('已连接文本，按顺序放在当前提示词前')).toContainText('1. 文本节点 2')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  expect(db.canvas.snapshot.formatVersion).toBe(1)
  await page.reload()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await expect(page.getByRole('textbox', { name: '文本节点 1内容' })).toHaveValue('海边')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await target.click({ position: { x: 60, y: 60 } })
  await page.screenshot({ path: testInfo.outputPath('text-node-desktop.png') })
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.submitted.length).toBe(1)
  expect(db.submitted[0]!.input.prompt).toBe('水彩\n\n海边\n\n一只小猫')
  await page.getByRole('textbox', { name: '文本节点 1内容' }).fill('雪山')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  expect(db.submitted[0]!.input.prompt).toBe('水彩\n\n海边\n\n一只小猫')
  expect(db.errors).toEqual([])
})

test('文生视频可以只使用文本节点，不需要媒体素材', async ({ page }) => {
  const initial: SavedCanvas = { id: canvasId, title: '视频文本', version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [],
    snapshot: { formatVersion: 1, viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: textA, position: { x: 50, y: 110 }, data: { kind: 'text', name: '场景文字', text: '雨中的街道' } },
      { id: videoId, position: { x: 600, y: 110 }, data: { kind: 'video', name: '视频目标', videoMode: 'text' } },
    ] } }
  const db = await setupCanvas(page, initial)
  await page.getByRole('article', { name: '视频目标', exact: true }).click({ position: { x: 60, y: 60 } })
  await expect(page.getByRole('button', { name: '参考', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '文本', exact: true }).click()
  await page.getByRole('article', { name: '场景文字', exact: true }).locator('.selection-overlay').click()
  await page.getByRole('button', { name: '返回节点' }).click()
  await page.getByRole('button', { name: '生成视频', exact: true }).click()
  await expect.poll(() => db.submitted.length).toBe(1)
  expect(db.submitted[0]!.input.prompt).toBe('雨中的街道')
  expect(db.submitted[0]!.input.mode).toBe('text')
  expect(db.errors).toEqual([])
})

test('文本和图片素材标签一起提交时，文字保持原文且素材标签仍是结构化引用', async ({ page }) => {
  const sourceId = '44444444-4444-4444-8444-444444444444'
  const targetId = '55555555-5555-4555-8555-555555555555'
  const initial: SavedCanvas = { id: canvasId, title: '文本和素材', version: 1, updatedAt: new Date().toISOString(), tasks: [], results: [],
    assets: [{ canvasId, nodeId: sourceId, key: 'sample.png', url: '/api/assets/content?key=sample.png', kind: 'images', source: 'uploads', contentType: 'image/png', bytes: imageBytes.length }],
    snapshot: { formatVersion: 1, viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: textA, position: { x: 40, y: 110 }, data: { kind: 'text', name: '文字', text: '场景 @保持原样' } },
      { id: targetId, position: { x: 550, y: 110 }, data: { kind: 'image', name: '目标', prompt: '参考 @素材', textReferenceIds: [textA], referenceIds: [sourceId], promptParts: [
        { type: 'text', text: '参考 ' }, { type: 'reference', nodeId: sourceId, kind: 'image', name: '素材' },
      ] } },
      { id: sourceId, position: { x: 1050, y: 110 }, data: { kind: 'image', name: '素材', origin: 'upload' } },
    ] } }
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  const db = await setupCanvas(page, initial)
  await expect(page.getByRole('article', { name: '素材', exact: true }).getByRole('img')).toBeVisible()
  await page.getByRole('article', { name: '目标', exact: true }).click({ position: { x: 60, y: 60 } })
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.submitted.length).toBe(1)
  expect(db.submitted[0]!.input.prompt).toBe('场景 @保持原样\n\n参考 @素材')
  expect(db.submitted[0]!.input.promptParts).toEqual([
    { type: 'text', text: '场景 @保持原样\n\n参考 ' },
    { type: 'reference', nodeId: sourceId, kind: 'image', name: '素材' },
  ])
  expect(db.submitted[0]!.input.referenceKeys).toEqual(['sample.png'])
})

test('首次保存文本尚未返回时删除它，后续保存保留新格式标记', async ({ page }) => {
  const db = await setupCanvas(page)
  const bodies: { formatVersion?: number; nodes: unknown[] }[] = []
  let releaseSave!: () => void
  const held = new Promise<void>((resolve) => { releaseSave = resolve })
  await page.route('**/api/canvases**', async (route) => {
    if (new URL(route.request().url()).pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() !== 'PUT') return route.fulfill({ json: db.canvas })
    const body = route.request().postDataJSON()
    bodies.push(body.snapshot)
    if (bodies.length === 1) await held
    if (body.version !== db.canvas.version || (db.canvas.snapshot.formatVersion === 1 && body.snapshot.formatVersion !== 1)) {
      return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '版本冲突' } } })
    }
    db.canvas.snapshot = body.snapshot
    db.canvas.version++
    return route.fulfill({ json: { version: db.canvas.version } })
  })
  try {
    await addNode(page, 60, 140, '文本')
    await expect.poll(() => bodies.length).toBe(1)
    await page.getByRole('article', { name: '文本节点 1', exact: true }).locator('.text-node-header').click({ button: 'right' })
    await page.getByRole('menuitem', { name: '删除', exact: true }).click()
    releaseSave()
    await expect.poll(() => bodies.length).toBe(2)
    await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
    expect(bodies.map((body) => body.formatVersion)).toEqual([1, 1])
    expect(db.canvas.snapshot.nodes).toHaveLength(0)
  } finally { releaseSave() }
})

test('复制生成节点可选择保留文本连接，删除源文字后清理连接', async ({ page }) => {
  const initial: SavedCanvas = { id: canvasId, title: '复制文本连接', version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [],
    snapshot: { formatVersion: 1, viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: textA, position: { x: 40, y: 110 }, data: { kind: 'text', name: '风格文字', text: '水彩' } },
      { id: videoId, position: { x: 550, y: 110 }, data: { kind: 'image', name: '图片目标', prompt: '小猫', textReferenceIds: [textA] } },
    ] } }
  const db = await setupCanvas(page, initial)
  const target = page.getByRole('article', { name: '图片目标', exact: true })
  async function copy(mode: '复制' | '复制副本', x: number) {
    await target.click({ button: 'right', position: { x: 60, y: 60 } })
    await page.getByRole('menuitem', { name: mode, exact: true }).click()
    await page.mouse.click(x, 540, { button: 'right' })
    await page.getByRole('menuitem', { name: '粘贴', exact: true }).click()
  }
  await copy('复制', 40)
  await copy('复制副本', 560)
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(4)
  const copies = db.canvas.snapshot.nodes.filter((node) => node.data.name === '图片目标 副本')
  expect(copies[0]!.data).not.toHaveProperty('textReferenceIds')
  expect(copies[1]!.data).toHaveProperty('textReferenceIds', [textA])
  await page.getByRole('article', { name: '风格文字', exact: true }).locator('.text-node-header').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(3)
  expect(db.canvas.snapshot.formatVersion).toBe(1)
  expect(db.canvas.snapshot.nodes.every((node) => node.data.kind === 'text' || !node.data.textReferenceIds?.length)).toBe(true)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
})

test('文本编辑不拖动节点或误删，拖动标题后位置可保存', async ({ page }) => {
  const db = await setupCanvas(page)
  await addNode(page, 80, 180, '文本')
  const node = page.getByRole('article', { name: '文本节点 1', exact: true })
  const input = node.getByRole('textbox')
  const before = (await node.boundingBox())!
  await input.fill('甲乙')
  await input.press('ArrowLeft')
  await input.press('Delete')
  await expect(input).toHaveValue('甲')
  expect((await node.boundingBox())!.x).toBeCloseTo(before.x, 1)
  const header = (await node.locator('.text-node-header').boundingBox())!
  await page.mouse.move(header.x + 60, header.y + 14)
  await page.mouse.down()
  await page.mouse.move(header.x + 160, header.y + 54, { steps: 8 })
  await page.mouse.up()
  await expect.poll(() => db.canvas.snapshot.nodes[0]?.position.x).toBeCloseTo(180, 1)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.reload()
  await expect(page.getByRole('textbox', { name: '文本节点 1内容' })).toHaveValue('甲')
  expect((await node.boundingBox())!.x).toBeCloseTo(180, 1)
})

test.describe('手机文本节点', () => {
  test.use({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true })

  test('长按创建、编辑和复制', async ({ page }, testInfo) => {
    const db = await setupCanvas(page)
    const client = await page.context().newCDPSession(page)
    async function longPress(x: number, y: number) {
      await client.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] })
      await page.waitForTimeout(600) // The menu intentionally opens after a 500 ms long press.
      await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    }
    try {
      await longPress(24, 150)
      await page.getByRole('menuitem', { name: '添加节点', exact: true }).tap()
      await page.getByRole('menuitem', { name: '文本', exact: true }).tap()
      const text = page.getByRole('article', { name: '文本节点 1', exact: true })
      await text.getByRole('textbox').fill('手机上写的文字')
      const box = (await text.boundingBox())!
      expect(box.x + box.width).toBeLessThanOrEqual(375)
      await page.screenshot({ path: testInfo.outputPath('text-node-mobile.png') })
      await longPress(box.x + 60, box.y + 14)
      await page.getByRole('menuitem', { name: '复制', exact: true }).tap()
      await longPress(25, 540)
      await page.getByRole('menuitem', { name: '粘贴', exact: true }).tap()
      await expect(page.getByRole('textbox', { name: '文本节点 1 副本内容' })).toHaveValue('手机上写的文字')
      await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
      expect(db.canvas.snapshot.nodes).toHaveLength(2)
      expect(db.errors).toEqual([])
    } finally { await client.detach() }
  })
})
