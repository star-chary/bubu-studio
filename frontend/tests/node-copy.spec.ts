import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { SavedCanvas } from '../src/api/persistence'
import type { StoredAsset } from '../src/api/assets'

const canvasID = '33333333-3333-4333-8333-333333333333'
const sourceA = '11111111-1111-4111-8111-111111111111'
const sourceB = '22222222-2222-4222-8222-222222222222'
const targetID = '44444444-4444-4444-8444-444444444444'
const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const article = (page: Page, name: string) => page.getByRole('article', { name, exact: true })
const saved = (page: Page) => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
const asset = (nodeId: string, source: 'uploads' | 'generated' = 'uploads'): StoredAsset => {
  const key = `${nodeId}.png`
  return { canvasId: canvasID, nodeId, key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, kind: 'images', source, contentType: 'image/png', bytes: imageBytes.length }
}

async function setup(page: Page, canvas: SavedCanvas) {
  let failCopy = false
  let copyCalls = 0
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/canvases**', (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    if (request.method() === 'PUT') {
      const body = request.postDataJSON()
      if (body.version !== canvas.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '版本冲突' } } })
      canvas.snapshot = body.snapshot
      canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    if (request.method() === 'POST' && path.endsWith('/asset-copy')) {
      copyCalls++
      if (failCopy) return route.fulfill({ status: 503, json: { error: { message: '素材复制失败，请重新保存。' } } })
      const id = path.split('/').at(-2)!
      const node = canvas.snapshot.nodes.find((item) => item.id === id)
      const source = canvas.assets.find((item) => item.key === node?.data.copySourceKey)
      if (!source) return route.fulfill({ status: 400, json: { error: { message: '源素材不存在' } } })
      const copied = asset(id, source.source)
      canvas.assets = [...canvas.assets.filter((item) => item.nodeId !== id), copied]
      return route.fulfill({ status: 201, json: copied })
    }
    return route.fulfill({ json: canvas })
  })
  await page.goto(`/?canvas=${canvasID}`)
  await saved(page)
  return { canvas, errors, failCopy(value: boolean) { failCopy = value }, get copyCalls() { return copyCalls } }
}

function initialCanvas(): SavedCanvas {
  return { id: canvasID, version: 1, updatedAt: new Date().toISOString(), assets: [asset(sourceA), asset(sourceB)], tasks: [], results: [], snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
    { id: sourceA, position: { x: 40, y: 120 }, data: { kind: 'image', name: '素材 A', origin: 'upload' } },
    { id: sourceB, position: { x: 545, y: 120 }, data: { kind: 'image', name: '素材 B', origin: 'upload' } },
    { id: targetID, position: { x: 1050, y: 120 }, data: { kind: 'image', name: '目标图片', prompt: '参考 @素材 A 和 @素材 B', referenceIds: [sourceA, sourceB], promptParts: [
      { type: 'text', text: '参考 ' }, { type: 'reference', nodeId: sourceA, kind: 'image', name: '素材 A' },
      { type: 'text', text: ' 和 ' }, { type: 'reference', nodeId: sourceB, kind: 'image', name: '素材 B' },
    ], modelSelection: { provider: 'apimart', modelId: '', parameters: { ratio: '16:9' } } } },
  ] } }
}

async function chooseCopy(page: Page, name: string, mode: '复制' | '复制副本') {
  await article(page, name).click({ button: 'right', position: { x: 80, y: 80 } })
  await page.getByRole('menu', { name: '节点菜单' }).getByRole('menuitem', { name: mode, exact: true }).click()
}
async function pasteAt(page: Page, x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menu', { name: '画布菜单' }).getByRole('menuitem', { name: '粘贴', exact: true }).click()
}

test('两个复制选项分别清除或保留参考，提示词和参数独立保存', async ({ page }) => {
  const db = await setup(page, initialCanvas())
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await article(page, '目标图片').click({ button: 'right', position: { x: 80, y: 80 } })
  const menu = page.getByRole('menu', { name: '节点菜单' })
  await expect(menu.getByRole('menuitem')).toHaveText(['复制', '复制副本', '删除 Delete'])
  await menu.getByRole('menuitem', { name: '复制', exact: true }).locator('.copy-help').hover()
  await expect(page.getByRole('tooltip').filter({ hasText: '仅复制节点本身' })).toBeVisible()
  await menu.getByRole('menuitem', { name: '复制副本', exact: true }).locator('.copy-help').hover()
  await expect(page.getByRole('tooltip').filter({ hasText: '复制节点并保留对原参考节点的连接' })).toBeVisible()
  await menu.getByRole('menuitem', { name: '复制', exact: true }).click()
  await pasteAt(page, 600, 660)
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(4)
  const single = db.canvas.snapshot.nodes[3]!
  expect(single.id).not.toBe(targetID)
  expect(single.data.referenceIds).toEqual([])
  expect(single.data.promptParts).toBeUndefined()
  expect(single.data.prompt).toBe('参考 @素材 A 和 @素材 B')
  expect(single.data.modelSelection?.parameters).toEqual({ ratio: '16:9' })
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await chooseCopy(page, '目标图片', '复制副本')
  await pasteAt(page, 40, 660)
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(5)
  const linked = db.canvas.snapshot.nodes[4]!
  expect(linked.id).not.toBe(single.id)
  expect(linked.data.referenceIds).toEqual([sourceA, sourceB])
  expect(linked.data.promptParts?.filter((part) => part.type === 'reference').map((part) => part.nodeId)).toEqual([sourceA, sourceB])
  await expect(page.locator('.vue-flow__edge')).toHaveCount(4)
  await page.reload()
  await saved(page)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(4)
  expect(db.errors).toEqual([])
})

test('已保存的生成结果复制后具有独立素材，删除原节点和刷新后仍可恢复', async ({ page }) => {
  const canvas = initialCanvas()
  canvas.snapshot.nodes = [canvas.snapshot.nodes[2]!]
  canvas.snapshot.nodes[0]!.data.referenceIds = []
  canvas.snapshot.nodes[0]!.data.promptParts = undefined
  canvas.snapshot.nodes[0]!.data.origin = 'generated'
  canvas.assets = [asset(targetID, 'generated')]
  const db = await setup(page, canvas)
  await article(page, '目标图片').getByRole('img').waitFor()
  await chooseCopy(page, '目标图片', '复制')
  await pasteAt(page, 80, 660)
  await expect.poll(() => db.copyCalls).toBe(1)
  await expect.poll(() => db.canvas.snapshot.nodes.find((node) => node.id !== targetID)?.data.copySourceKey).toBeUndefined()
  const copied = db.canvas.snapshot.nodes.find((node) => node.id !== targetID)!
  expect(db.canvas.assets.find((item) => item.nodeId === copied.id)?.key).not.toBe(asset(targetID).key)
  await chooseCopy(page, '目标图片', '复制')
  await article(page, '目标图片').click({ button: 'right', position: { x: 80, y: 80 } })
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await saved(page)
  await page.reload()
  await saved(page)
  await expect(article(page, '目标图片 副本').getByRole('img')).toBeVisible()
  expect(db.canvas.snapshot.nodes).toHaveLength(1)
  expect(db.errors).toEqual([])
})

test('素材复制失败后保存待处理状态，刷新可重试并恢复', async ({ page }) => {
  const canvas = initialCanvas()
  canvas.snapshot.nodes = [canvas.snapshot.nodes[0]!]
  canvas.assets = [asset(sourceA)]
  const db = await setup(page, canvas)
  db.failCopy(true)
  await chooseCopy(page, '素材 A', '复制副本')
  await pasteAt(page, 650, 660)
  await expect(article(page, '素材 A 副本')).toContainText('素材复制失败')
  await expect.poll(() => db.canvas.snapshot.nodes.find((node) => node.id !== sourceA)?.data.copySourceKey).toBe(asset(sourceA).key)
  db.failCopy(false)
  await page.reload()
  await expect.poll(() => db.copyCalls).toBe(2)
  await expect.poll(() => db.canvas.snapshot.nodes.find((node) => node.id !== sourceA)?.data.copySourceKey).toBeUndefined()
  await expect(article(page, '素材 A 副本').getByRole('img')).toBeVisible()
  expect(db.errors).toEqual([])
})

test('本地上传素材复制后删除原节点，副本仍保留独立预览', async ({ page }) => {
  await page.goto(`/?temporary=${canvasID}`)
  await page.mouse.click(80, 150, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles({ name: '原图.png', mimeType: 'image/png', buffer: imageBytes })
  await expect(article(page, '原图.png').getByRole('img')).toBeVisible()
  await chooseCopy(page, '原图.png', '复制')
  await article(page, '原图.png').click({ button: 'right', position: { x: 80, y: 80 } })
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await pasteAt(page, 650, 660)
  await expect(article(page, '原图.png 副本').getByRole('img')).toBeVisible()
  await expect(article(page, '原图.png 副本')).toContainText('960 × 540')
})

test('视频复制副本保留起始帧，单独复制清除起始帧', async ({ page }) => {
  const canvas = initialCanvas()
  const target = canvas.snapshot.nodes[2]!
  target.data = { kind: 'video', name: '目标视频', prompt: '运动', origin: undefined, videoMode: 'image', firstFrameId: sourceA,
    referenceIds: [sourceA], modelSelection: { provider: 'apimart', modelId: '', parameters: {} } }
  const db = await setup(page, canvas)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  await chooseCopy(page, '目标视频', '复制副本')
  await pasteAt(page, 600, 660)
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(4)
  expect(db.canvas.snapshot.nodes[3]!.data.firstFrameId).toBe(sourceA)
  expect(db.canvas.snapshot.nodes[3]!.data.referenceIds).toEqual([sourceA])
  await chooseCopy(page, '目标视频', '复制')
  await pasteAt(page, 40, 660)
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(5)
  expect(db.canvas.snapshot.nodes[4]!.data.firstFrameId).toBeUndefined()
  expect(db.canvas.snapshot.nodes[4]!.data.referenceIds).toEqual([])
})

test('复制副本后参考节点被删除，粘贴保留有效引用并提示缺失', async ({ page }) => {
  const db = await setup(page, initialCanvas())
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await chooseCopy(page, '目标图片', '复制副本')
  await article(page, '素材 A').click({ button: 'right', position: { x: 80, y: 80 } })
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await pasteAt(page, 40, 660)
  await expect(page.getByRole('alert').filter({ hasText: '部分参考节点已不存在' })).toBeVisible()
  await expect.poll(() => db.canvas.snapshot.nodes.find((node) => node.data.name === '目标图片 副本')?.data.referenceIds).toEqual([sourceB])
  const copied = db.canvas.snapshot.nodes.find((node) => node.data.name === '目标图片 副本')!
  expect(copied.data.promptParts?.some((part) => part.type === 'reference' && part.nodeId === sourceA)).toBe(true)
  await page.reload()
  await saved(page)
  await expect(article(page, '目标图片 副本')).toBeVisible()
})

for (const file of [
  { name: 'sample-video.mp4', label: '视频' },
  { name: 'reference-audio.mp3', label: '音频' },
]) {
  test(`本地上传的${file.label}节点可复制并保留独立播放素材`, async ({ page }) => {
    await page.goto(`/?temporary=${canvasID}`)
    await page.mouse.click(80, 150, { button: 'right' })
    const chooser = page.waitForEvent('filechooser')
    await page.getByRole('menuitem', { name: '上传', exact: true }).click()
    await (await chooser).setFiles(fileURLToPath(new URL(`./fixtures/${file.name}`, import.meta.url)))
    await expect(article(page, file.name).getByRole('button', { name: `播放${file.label}` })).toBeVisible()
    await chooseCopy(page, file.name, '复制副本')
    await article(page, file.name).click({ button: 'right', position: { x: 80, y: 80 } })
    await page.getByRole('menuitem', { name: '删除', exact: true }).click()
    await pasteAt(page, 650, 660)
    await expect(article(page, `${file.name} 副本`).getByRole('button', { name: `播放${file.label}` })).toBeVisible()
  })
}

test('复制生成中的节点不复制后台任务，也不自动生成', async ({ page }) => {
  const canvas = initialCanvas()
  canvas.tasks = [{ id: '55555555-5555-4555-8555-555555555555', kind: 'image', status: 'running',
    input: { canvasId: canvasID, nodeId: targetID, model: 'test-model', prompt: '正在生成' },
    createdAt: '2026-11-12T00:00:00Z', updatedAt: '2026-11-12T00:00:00Z' }]
  const db = await setup(page, canvas)
  await expect(article(page, '目标图片')).toContainText('图片生成中')
  await chooseCopy(page, '目标图片', '复制副本')
  await pasteAt(page, 600, 660)
  await expect(article(page, '目标图片 副本')).toBeVisible()
  await expect(article(page, '目标图片 副本')).not.toContainText('图片生成中')
  await expect.poll(() => db.canvas.snapshot.nodes.length).toBe(4)
  expect(db.canvas.tasks).toHaveLength(1)
  expect(db.copyCalls).toBe(0)
})
