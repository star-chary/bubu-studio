import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Locator, Page, Route } from '@playwright/test'
import { expect, test } from './setup'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'
import type { StoredAsset } from '../src/api/assets'

const canvasID = '33333333-3333-4333-8333-333333333333'
const nodeID = '11111111-1111-4111-8111-111111111111'
const taskID = '77777777-7777-4777-8777-777777777777'
const fixtures = {
  image: { bytes: readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url))), type: 'image/png', extension: 'png', dimensions: '960 × 540', name: '生成图片' },
  video: { bytes: readFileSync(fileURLToPath(new URL('./fixtures/sample-video.mp4', import.meta.url))), type: 'video/mp4', extension: 'mp4', dimensions: '320 × 240', name: '生成视频' },
}
type Kind = keyof typeof fixtures

async function fulfillMedia(route: Route, kind: Kind) {
  await route.fulfill({ contentType: fixtures[kind].type, body: fixtures[kind].bytes, headers: { 'Cache-Control': 'private, no-store' } })
}

async function setup(page: Page, kind: Kind, read: (route: Route, number: number) => void | Promise<void>, options: { upload?: boolean; temporaryURL?: string; running?: boolean } = {}) {
  const fixture = fixtures[kind]
  const key = `existing-${nodeID}.${fixture.extension}`
  const asset: StoredAsset = { key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, canvasId: canvasID, nodeId: nodeID,
    kind: kind === 'image' ? 'images' : 'videos', source: options.upload ? 'uploads' : 'generated', contentType: fixture.type, bytes: fixture.bytes.length }
  const model = kind === 'image' ? 'doubao-seedream-5-0-260128' : 'doubao-seedance-2-5-260628'
  const task: GenerationTask = { id: taskID, kind, input: { canvasId: canvasID, nodeId: nodeID, model, prompt: '已完成的生成任务' },
    status: options.running ? 'running' : 'succeeded', creditStatus: 'settled', creditPoints: 6,
    result: { url: options.temporaryURL ?? asset.url, model, ...(options.temporaryURL ? { storageError: '图片已生成，尚未保存到云端。' } : { asset }) },
    createdAt: '2026-10-10T00:00:00Z', updatedAt: '2026-10-10T00:01:00Z' }
  const canvas: SavedCanvas = { id: canvasID, title: '预览恢复专项', version: 1, updatedAt: task.updatedAt, assets: [asset],
    tasks: options.upload ? [] : [task], results: options.upload || options.running ? [] : [task],
    snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: nodeID, position: { x: 100, y: 150 }, data: {
      kind, name: fixture.name, origin: options.upload ? 'upload' : 'generated', prompt: '保留原提示词',
    } }] } }
  if (options.running) canvas.assets = []
  const reads: { url: URL; method: string }[] = []
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => { if (!['GET', 'HEAD'].includes(request.method())) writes.push(`${request.method()} ${new URL(request.url()).pathname}`) })
  const handleRead = (route: Route) => {
    reads.push({ url: new URL(route.request().url()), method: route.request().method() })
    return read(route, reads.length)
  }
  await page.route('**/api/assets/content?*', handleRead)
  if (options.temporaryURL) await page.route(options.temporaryURL, handleRead)
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', async (route) => {
    if (new URL(route.request().url()).pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot
      return route.fulfill({ json: { version: ++canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  // 素材响应故意悬挂，页面本身不能等待全部图片 / 视频完成后才返回。
  await page.goto(`/?canvas=${canvasID}`, { waitUntil: 'domcontentloaded' })
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  return { asset, task, canvas, reads, writes, errors, node: page.getByRole('article', { name: fixture.name, exact: true }) }
}

async function expectReady(node: Locator, kind: Kind) {
  await expect(node).toContainText(fixtures[kind].dimensions)
  await expect(node.locator('.media-loading, .media-error')).toHaveCount(0)
  if (kind === 'image') await expect.poll(() => node.locator('img').evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0)).toBe(true)
  else {
    await expect(node.getByRole('button', { name: '播放视频', exact: true })).toBeVisible()
    await expect.poll(() => node.locator('video').evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2)
  }
}

for (const kind of ['image', 'video'] as const) {
  test(`${fixtures[kind].name}等待超过 30 秒仅提示慢，原请求随后成功仍正常显示`, async ({ page }, info) => {
    await page.clock.install()
    const pending: Route[] = []
    const db = await setup(page, kind, (route) => { pending.push(route) })
    await expect.poll(() => pending.length).toBeGreaterThan(0)
    const media = db.node.locator(kind === 'image' ? 'img' : 'video')
    await media.evaluate((element) => { element.setAttribute('data-original-preview', 'true') })
    const source = await media.getAttribute('src')
    await expect(db.node.getByRole('button', { name: '重新加载预览' })).toHaveCount(0)
    await page.clock.fastForward(31_000)
    await expect(db.node.getByRole('status')).toHaveText('素材已保存，预览加载较慢。')
    await expect(media).toHaveAttribute('src', source!)
    await expect(media).toHaveAttribute('data-original-preview', 'true')
    await expect(db.node.getByRole('alert')).toHaveCount(0)
    await expect(db.node.getByRole('button', { name: '重新加载预览' })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`${kind}-preview-slow.png`) })
    for (const route of pending) await fulfillMedia(route, kind)
    await expectReady(db.node, kind)
    await page.clock.fastForward(31_000)
    await expect(db.node.locator('.media-loading, .media-error')).toHaveCount(0)
    expect(db.writes).toEqual([])
    expect(db.task.status).toBe('succeeded')
    expect(db.task.creditStatus).toBe('settled')
    expect(db.errors).toEqual([])
  })

  test(`${fixtures[kind].name}真实读取失败可反复重试，成功后只恢复原素材，不创建任务或扣分`, async ({ page }, info) => {
    let fail = true
    const db = await setup(page, kind, (route) => fail
      ? route.fulfill({ status: 503, json: { error: { code: 'STORAGE_TIMEOUT', message: '素材读取超时' } } })
      : fulfillMedia(route, kind))
    await expect(db.node.getByRole('alert')).toHaveText('素材已保存，预览加载失败，请重新加载。')
    await expect(db.node.locator('img, video')).toHaveCount(0)
    await page.screenshot({ path: info.outputPath(`${kind}-preview-failed.png`) })
    const before = await db.node.boundingBox()
    await db.node.getByRole('button', { name: '重新加载预览' }).click()
    await expect.poll(() => db.reads.some((read) => read.url.searchParams.get('previewAttempt') === '1')).toBe(true)
    await expect(db.node.getByRole('alert')).toContainText('预览加载失败')
    expect(await db.node.boundingBox()).toEqual(before)
    fail = false
    await db.node.getByRole('button', { name: '重新加载预览' }).focus()
    await page.keyboard.press('Enter')
    await expectReady(db.node, kind)
    expect(db.reads.every((read) => read.method === 'GET' && read.url.searchParams.get('key') === db.asset.key)).toBe(true)
    expect(db.reads.some((read) => read.url.searchParams.get('previewAttempt') === '2')).toBe(true)
    expect(db.writes).toEqual([])
    expect(db.canvas.tasks).toHaveLength(1)
    expect(db.task.creditStatus).toBe('settled')
    expect(db.canvas.snapshot.nodes[0]!.data.prompt).toBe('保留原提示词')
    await expect(page.getByText('积分 200', { exact: true })).toBeVisible()
    if (kind === 'video') {
      await db.node.getByRole('button', { name: '播放视频', exact: true }).click()
      await expect.poll(() => db.node.locator('video').evaluate((element: HTMLVideoElement) => element.paused)).toBe(false)
    }
    expect(db.errors).toEqual([])
  })

  test(`${fixtures[kind].name}慢加载时可重新请求，旧预览的迟到错误不覆盖新预览`, async ({ page }) => {
    await page.clock.install()
    const pending: Route[] = []
    const db = await setup(page, kind, (route) => { pending.push(route) })
    await expect.poll(() => pending.length).toBeGreaterThan(0)
    const oldMedia = await db.node.locator(kind === 'image' ? 'img' : 'video').elementHandle()
    await page.clock.fastForward(31_000)
    await db.node.getByRole('button', { name: '重新加载预览' }).click()
    await expect.poll(() => pending.some((route) => new URL(route.request().url()).searchParams.has('previewAttempt'))).toBe(true)
    await expect(db.node.getByRole('status')).toHaveText('正在读取…')
    await expect(db.node.getByRole('button', { name: '重新加载预览' })).toHaveCount(0)
    await oldMedia!.dispatchEvent('error')
    await expect(db.node.getByRole('alert')).toHaveCount(0)
    await page.clock.fastForward(31_000)
    await expect(db.node.getByRole('status')).toHaveText('素材已保存，预览加载较慢。')
    for (const route of pending.filter((route) => new URL(route.request().url()).searchParams.has('previewAttempt'))) await fulfillMedia(route, kind)
    await expectReady(db.node, kind)
    await oldMedia!.dispatchEvent('error')
    for (const route of pending.filter((route) => !new URL(route.request().url()).searchParams.has('previewAttempt'))) await route.abort('failed')
    await expectReady(db.node, kind)
    expect(db.writes).toEqual([])
    expect(db.errors).toEqual([])
  })
}

test('刷新恢复的已保存素材即使没有生成任务，也有慢加载提示和预览重试', async ({ page }) => {
  await page.clock.install()
  let pending: Route | undefined
  const db = await setup(page, 'image', (route, number) => {
    if (number === 1) { pending = route; return }
    return fulfillMedia(route, 'image')
  }, { upload: true })
  await expect.poll(() => !!pending).toBe(true)
  await page.clock.fastForward(31_000)
  await expect(db.node.getByRole('status')).toHaveText('素材已保存，预览加载较慢。')
  await db.node.getByRole('button', { name: '重新加载预览' }).click()
  await expectReady(db.node, 'image')
  expect(db.writes).toEqual([])
  expect(db.canvas.tasks).toEqual([])
  await pending!.abort('failed')
  await page.reload()
  await expectReady(db.node, 'image')
  expect(db.reads.at(-1)!.url.searchParams.has('previewAttempt')).toBe(false)
  expect(db.errors).toEqual([])
})

test('临时生成图片重试保留签名原地址，不错误回退到节点旧的已保存素材', async ({ page }) => {
  const temporaryURL = 'https://provider.example.test/result.png?signature=keep-original'
  let fail = true
  const db = await setup(page, 'image', (route) => fail
    ? route.fulfill({ status: 502, body: 'temporary network failure' }) : fulfillMedia(route, 'image'), { temporaryURL })
  await expect(db.node.locator('.media-error')).toContainText('素材已生成，预览加载失败')
  await expect(db.node.locator('.asset-save-error')).toContainText('尚未保存到云端')
  fail = false
  await db.node.getByRole('button', { name: '重新加载预览' }).click()
  await expectReady(db.node, 'image')
  expect(db.reads.length).toBeGreaterThanOrEqual(2)
  expect(db.reads.every((read) => read.url.href === temporaryURL)).toBe(true)
  expect(db.writes).toEqual([])
  expect(db.errors).toEqual([])
})

test('本次生成完成后的新结果同样能在超过 30 秒后继续加载成功', async ({ page }) => {
  await page.clock.install()
  let pending: Route | undefined
  const db = await setup(page, 'image', (route) => { pending = route }, { running: true })
  await expect(db.node.getByRole('status')).toContainText('生成')
  db.task.status = 'succeeded'
  db.task.updatedAt = '2026-10-10T00:02:00Z'
  await page.clock.fastForward(2500)
  await expect.poll(() => !!pending).toBe(true)
  await page.clock.fastForward(31_000)
  await expect(db.node.getByRole('status')).toHaveText('素材已保存，预览加载较慢。')
  await fulfillMedia(pending!, 'image')
  await expectReady(db.node, 'image')
  expect(db.writes).toEqual([])
  expect(db.errors).toEqual([])
})
