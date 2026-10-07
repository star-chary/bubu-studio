import type { Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { test, expect } from './setup'

const imagePath = fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url))
const videoPath = fileURLToPath(new URL('./fixtures/sample-video.mp4', import.meta.url))
const imageBytes = readFileSync(imagePath)
const makeAsset = (canvasId: string, nodeId: string, kind = 'images', source = 'uploads') => {
  const key = `frame-space/dev/canvases/${canvasId}/${source}/${kind}/${nodeId}/e753b4e4-2fe1-41e1-ab3b-f3e6e57c4b82.${kind === 'images' ? 'png' : 'mp4'}`
  return { key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, kind, source, canvasId, nodeId, contentType: kind === 'images' ? 'image/png' : 'video/mp4', bytes: 123 }
}

async function upload(page: Page, files: string[]) {
  await page.mouse.click(90, 150, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(files)
}
async function prepareImageNode(page: Page) {
  await page.mouse.click(380, 150, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const node = page.getByRole('article', { name: '图片节点 1', exact: true })
  await node.click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox').fill('湖面上的木屋')
  return node
}
async function useSavedCanvas(page: Page) {
  const id = '33333333-3333-4333-8333-333333333333'
  const canvas = { id, title: '存储测试', version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [],
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
  await page.route('**/api/storage/config', (route) => route.fulfill({ json: { enabled: true, maxImageBytes: 20 << 20, maxVideoBytes: 200 << 20 } }))
})

test('批量导入立即预览，按画布和节点归属保存，最多两个上传并行且不发送密钥', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => { release = resolve })
  let calls = 0
  let active = 0
  let maximum = 0
  const canvases: string[] = []
  const nodes: string[] = []
  await page.route('**/api/assets?*', async (route) => {
    calls++; active++; maximum = Math.max(maximum, active)
    const request = route.request()
    expect(request.headers().authorization).toBeUndefined()
    expect(request.headers()['content-type']).toBe('application/octet-stream')
    const query = new URL(request.url()).searchParams
    const canvasId = query.get('canvasId')!
    const nodeId = query.get('nodeId')!
    canvases.push(canvasId); nodes.push(nodeId)
    expect(canvasId).toMatch(/^[0-9a-f-]{36}$/)
    expect(nodeId).toMatch(/^[0-9a-f-]{36}$/)
    const kind = request.postDataBuffer()?.equals(imageBytes) ? 'images' : 'videos'
    await pending
    active--
    await route.fulfill({ status: 201, json: makeAsset(canvasId, nodeId, kind) })
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, [imagePath, videoPath, imagePath])
  await expect(page.getByRole('article')).toHaveCount(3)
  await expect(page.getByRole('article', { name: 'sample-video.mp4', exact: true })).toContainText('320 × 240')
  await expect.poll(() => calls).toBe(2)
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(2)
  release()
  await expect.poll(() => calls).toBe(3)
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(0)
  await expect(page.getByText('已保存', { exact: true })).toHaveCount(0)
  expect(calls).toBe(3)
  expect(maximum).toBe(2)
  expect(new Set(canvases).size).toBe(1)
  expect(new Set(nodes).size).toBe(3)
  await expect(page.getByRole('article', { name: 'sample-image.png', exact: true }).first().locator('img')).toHaveAttribute('src', /^blob:/)
  const video = page.getByRole('article', { name: 'sample-video.mp4', exact: true })
  await video.getByRole('button', { name: '播放视频', exact: true }).click()
  await expect(video.getByRole('button', { name: '暂停视频', exact: true })).toBeVisible()
})

for (const failure of [
  { status: 504, code: 'STORAGE_TIMEOUT', message: '连接或传输到 OSS 超时，请检查后端网络、VPN/代理连接后重新保存。' },
  { status: 503, code: 'STORAGE_ACCESS_DENIED', message: 'OSS 拒绝访问，请检查后端账号权限及存储桶访问策略。' },
]) {
  test(`${failure.code} 显示具体原因并保留预览，重新保存不新增节点或调用模型`, async ({ page }, testInfo) => {
    let calls = 0
    let generations = 0
    page.on('request', (request) => { if (request.url().endsWith('/api/images/generations')) generations++ })
    await page.route('**/api/assets?*', async (route) => {
      calls++
      if (calls === 1) await route.fulfill({ status: failure.status, json: { error: { code: failure.code, message: failure.message } } })
      else {
        const query = new URL(route.request().url()).searchParams
        await route.fulfill({ status: 201, json: makeAsset(query.get('canvasId')!, query.get('nodeId')!) })
      }
    })
    await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
    await upload(page, [imagePath])
    const node = page.getByRole('article', { name: 'sample-image.png', exact: true })
    await expect(node.getByRole('alert')).toContainText(failure.message)
    await expect(node.getByRole('img')).toHaveAttribute('src', /^blob:/)
    const box = await node.boundingBox()
    await page.screenshot({ path: testInfo.outputPath('upload-save-error.png') })
    await node.getByRole('button', { name: '重新保存', exact: true }).click()
    await expect(node.getByText('保存中…', { exact: true })).toHaveCount(0)
    await expect(node.getByRole('alert')).toHaveCount(0)
    await expect(page.getByRole('article')).toHaveCount(1)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(await node.boundingBox()).toEqual(box)
    expect(calls).toBe(2)
    expect(generations).toBe(0)
  })
}

test('生成结果展示已转存的同源图片，图片归属与生成节点一致', async ({ page }, testInfo) => {
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/images/generations', (route) => {
    const input = route.request().postDataJSON()
    const asset = makeAsset(input.canvasId, input.nodeId, 'images', 'generated')
    const { taskId, acceptedPoints, priceVersion, ...taskInput } = input
    const now = new Date().toISOString()
    return route.fulfill({ status: 202, json: { id: taskId, kind: 'image', input: taskInput, status: 'succeeded',
      result: { url: asset.url, model: input.model, asset }, creditPoints: acceptedPoints, creditPriceVersion: priceVersion,
      creditStatus: 'settled', createdAt: now, updatedAt: now } })
  })
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  const node = await prepareImageNode(page)
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(node.getByRole('img')).toHaveAttribute('src', /^\/api\/assets\/content\?key=/)
  await expect(node).toContainText('960 × 540')
  await expect(node.getByText('已保存', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('generated-saved-asset.png') })
})

test('转存失败保留任务错误与草稿，不展示不可交付的临时图片或自动重试模型', async ({ page }) => {
  let calls = 0
  const temporaryURL = 'https://temporary.example.test/generated.png'
  await page.route(temporaryURL, (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/images/generations', (route) => {
    calls++
    const input = route.request().postDataJSON()
    const { taskId, acceptedPoints, priceVersion, ...taskInput } = input
    const now = new Date().toISOString()
    return route.fulfill({ status: 202, json: { id: taskId, kind: 'image', input: taskInput, status: 'storage_failed',
      result: { url: temporaryURL, model: input.model }, error: { code: 'STORAGE_FAILED', message: '图片已生成，但云端保存失败。' },
      creditPoints: acceptedPoints, creditPriceVersion: priceVersion, creditStatus: 'review', createdAt: now, updatedAt: now } })
  })
  await useSavedCanvas(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  const node = await prepareImageNode(page)
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '图片提示词' }).getByRole('alert')).toContainText('云端保存失败')
  await expect(node.getByRole('img')).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('湖面上的木屋')
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  expect(calls).toBe(1)
})
