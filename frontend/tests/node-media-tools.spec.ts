import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page, Route } from '@playwright/test'
import { expect, test } from './setup'
import type { SavedCanvas } from '../src/api/persistence'
import type { StoredAsset } from '../src/api/assets'

const canvasId = '33333333-3333-4333-8333-333333333333'
const nodeId = '11111111-1111-4111-8111-111111111111'
const fixtures = {
  image: { label: '图片', file: 'sample-image.png', type: 'image/png', extension: 'png', dimensions: '960 × 540' },
  video: { label: '视频', file: 'sample-video.mp4', type: 'video/mp4', extension: 'mp4', dimensions: '320 × 240' },
  audio: { label: '音频', file: 'reference-audio.wav', type: 'audio/wav', extension: 'wav', dimensions: '' },
}
type Kind = keyof typeof fixtures
const fixturePath = (kind: Kind) => fileURLToPath(new URL(`./fixtures/${fixtures[kind].file}`, import.meta.url))
const toolbar = (page: Page) => page.getByRole('toolbar', { name: /的素材工具$/ })

async function addNode(page: Page, kind: '图片' | '视频', x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
}

async function savedMedia(page: Page, kind: 'image' | 'video', read?: (route: Route) => Promise<void>) {
  const fixture = fixtures[kind]
  const bytes = readFileSync(fixturePath(kind))
  const key = `generated-${nodeId}.${fixture.extension}`
  const asset: StoredAsset = { key, url: `/api/assets/content?key=${key}`, canvasId, nodeId, kind: kind === 'image' ? 'images' : 'videos',
    source: 'generated', contentType: fixture.type, bytes: bytes.length }
  const canvas: SavedCanvas = { id: canvasId, title: '节点素材工具专项', version: 1, updatedAt: '2026-10-11T00:00:00Z', assets: [asset], tasks: [], results: [],
    snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: nodeId, position: { x: 380, y: 150 }, data: {
      name: `生成${fixture.label}`, kind, origin: 'generated', prompt: '保留这个节点的提示词', videoMode: 'text',
    } }] } }
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => { if (!['GET', 'HEAD'].includes(request.method())) writes.push(`${request.method()} ${new URL(request.url()).pathname}`) })
  await page.route('**/api/assets/content?*', read ?? ((route) => route.fulfill({ contentType: fixture.type, body: bytes })))
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', (route) => {
    if (new URL(route.request().url()).pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') return route.fulfill({ json: { version: ++canvas.version } })
    return route.fulfill({ json: canvas })
  })
  await page.goto(`/?canvas=${canvasId}`)
  return { bytes, asset, writes, errors, node: page.getByRole('article', { name: `生成${fixture.label}`, exact: true }) }
}

for (const kind of ['image', 'video'] as const) {
  test(`生成${fixtures[kind].label}下载原文件，全屏预览退出后保留提示词和画布位置`, async ({ page }, info) => {
    const fixture = fixtures[kind]
    const db = await savedMedia(page, kind)
    await expect(db.node).toContainText(fixture.dimensions)
    await db.node.click({ position: { x: 100, y: 100 } })
    const tools = toolbar(page)
    await expect(tools.getByRole('button')).toHaveCount(2)
    const nodeBox = (await db.node.boundingBox())!
    const toolBox = (await tools.boundingBox())!
    expect(toolBox.y + toolBox.height).toBeLessThan(nodeBox.y)
    const prompt = page.getByRole('dialog', { name: `${fixture.label}提示词` })
    await expect(prompt).toBeVisible()
    const promptBox = (await prompt.boundingBox())!
    expect(promptBox.y).toBeCloseTo(nodeBox.y + nodeBox.height + 12, 1)
    const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')
    await page.screenshot({ path: info.outputPath(`${kind}-node-tools.png`) })

    const downloadEvent = page.waitForEvent('download')
    await tools.getByRole('button', { name: '下载素材' }).click()
    const download = await downloadEvent
    expect(download.suggestedFilename()).toBe(`生成${fixture.label}.${fixture.extension}`)
    expect(readFileSync((await download.path())!)).toEqual(db.bytes)
    await expect(prompt).toBeVisible()
    await expect(tools.getByRole('button', { name: '下载素材' })).toBeEnabled()
    if (kind === 'video') {
      await db.node.getByRole('button', { name: '播放视频', exact: true }).click()
      await expect.poll(() => db.node.locator('video').evaluate((el: HTMLVideoElement) => el.paused)).toBe(false)
      // 播放控件按原有规则关闭输入框，重新点击画面后再验证工具不会关闭面板。
      await db.node.click({ position: { x: 100, y: 100 } })
      await expect(prompt).toBeVisible()
    }
    await tools.getByRole('button', { name: '全屏预览', exact: true }).click()
    const preview = page.getByRole('dialog', { name: `${fixture.label}全屏预览` })
    await expect(preview).toBeVisible()
    expect(await preview.boundingBox()).toEqual({ x: 0, y: 0, width: 1440, height: 900 })
    await expect(preview.getByRole('status')).toHaveCount(0)
    await expect(preview.getByRole('button', { name: '关闭全屏预览' })).toBeFocused()
    await page.keyboard.press('Delete')
    const media = preview.locator(kind === 'image' ? 'img' : 'video')
    if (kind === 'image') {
      expect(await media.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(960)
      await page.screenshot({ path: info.outputPath('image-fullscreen.png') })
    } else {
      expect(await db.node.locator('video').evaluate((el: HTMLVideoElement) => el.paused)).toBe(true)
      await expect(media).toHaveAttribute('controls', '')
      await media.evaluate(async (el: HTMLVideoElement) => { el.currentTime = .5; await el.play() })
      await expect.poll(() => media.evaluate((el: HTMLVideoElement) => el.currentTime)).toBeGreaterThan(.5)
      await media.evaluate((el: HTMLVideoElement) => el.pause())
      await page.screenshot({ path: info.outputPath('video-fullscreen.png') })
    }
    const previousPlayer = kind === 'video' ? await media.elementHandle() : null
    await page.keyboard.press('Escape')
    await expect(preview).toHaveCount(0)
    if (previousPlayer) expect(await previousPlayer.evaluate((el: HTMLVideoElement) => el.paused && !el.getAttribute('src'))).toBe(true)
    await expect(tools.getByRole('button', { name: '全屏预览' })).toBeFocused()
    await expect(prompt.getByRole('textbox')).toHaveText('保留这个节点的提示词')
    expect(await db.node.boundingBox()).toEqual(nodeBox)
    expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
    await expect(page.getByText('积分 200', { exact: true })).toBeVisible()
    expect(db.writes.every((write) => write === 'POST /api/credits/quote' || write === `PUT /api/canvases/${canvasId}`)).toBe(true)
    expect(db.errors).toEqual([])
  })
}

test('上传图片、视频和音频都有素材工具，下载与全屏预览不打开输入框', async ({ page }) => {
  await page.goto(`/?temporary=${canvasId}`)
  await page.mouse.click(70, 160, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles([fixturePath('image'), fixturePath('video'), fixturePath('audio')])
  const writes: string[] = []
  page.on('request', (request) => { if (!['GET', 'HEAD'].includes(request.method())) writes.push(request.url()) })
  for (const kind of ['image', 'video', 'audio'] as const) {
    const fixture = fixtures[kind]
    const node = page.getByRole('article', { name: fixture.file, exact: true })
    await expect(node.locator('.media-loading, .media-error')).toHaveCount(0)
    await node.click({ position: { x: 65, y: 70 } })
    await expect(toolbar(page)).toHaveAttribute('aria-label', `${fixture.file}的素材工具`)
    await expect(page.locator('.node-prompt-panel')).toHaveCount(0)
    const downloadEvent = page.waitForEvent('download')
    await toolbar(page).getByRole('button', { name: '下载素材' }).click()
    const download = await downloadEvent
    expect(download.suggestedFilename()).toBe(fixture.file)
    expect(readFileSync((await download.path())!)).toEqual(readFileSync(fixturePath(kind)))
    await toolbar(page).getByRole('button', { name: '全屏预览', exact: true }).click()
    const preview = page.getByRole('dialog', { name: `${fixture.label}全屏预览` })
    await expect(preview).toBeVisible()
    await expect(preview.getByRole('status')).toHaveCount(0)
    await preview.getByRole('button', { name: '关闭全屏预览' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
  }
  await page.mouse.click(60, 850)
  await expect(toolbar(page)).toHaveCount(0)
  expect(writes).toEqual([])
})

test('空节点工具置灰，工具跟随拖动缩放，参考选择仍只添加参考', async ({ page }) => {
  await page.goto(`/?temporary=${canvasId}`)
  await page.mouse.click(80, 130, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(fixturePath('image'))
  await addNode(page, '图片', 800, 150)
  await addNode(page, '视频', 800, 600)
  for (const name of ['视频节点 2', '图片节点 1']) {
    await page.getByRole('article', { name, exact: true }).click({ position: { x: 80, y: 80 } })
    await expect(toolbar(page).getByRole('button', { name: '下载素材' })).toBeDisabled()
    await expect(toolbar(page).getByRole('button', { name: '全屏预览' })).toBeDisabled()
  }
  const target = page.getByRole('article', { name: '图片节点 1', exact: true })
  const prompt = page.getByRole('dialog', { name: '图片提示词' })
  await prompt.getByRole('button', { name: '参考', exact: true }).click()
  await expect(toolbar(page)).toHaveCount(0)
  await page.getByRole('article', { name: fixtures.image.file, exact: true }).click({ position: { x: 80, y: 80 } })
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  await expect(toolbar(page)).toHaveCount(0)
  await expect(prompt).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(toolbar(page)).toHaveAttribute('aria-label', '图片节点 1的素材工具')
  const before = (await target.boundingBox())!
  await page.mouse.move(before.x + 60, before.y + 80)
  await page.mouse.down()
  await page.mouse.move(before.x + 110, before.y + 120, { steps: 6 })
  await page.mouse.up()
  await expect(prompt).toHaveCount(0)
  await page.getByRole('button', { name: '缩小画布', exact: true }).click()
  const after = (await target.boundingBox())!
  const tools = (await toolbar(page).boundingBox())!
  expect(tools.y + tools.height).toBeCloseTo(after.y - 8, 1)
  expect(tools.x + tools.width / 2).toBeCloseTo(after.x + after.width / 2, 1)
})

test('预览读取失败仍能操作已有素材，下载失败不保存错误响应，恢复后可重试', async ({ page }) => {
  let mode: 'failed' | 'invalid' | 'ready' = 'failed'
  let downloads = 0
  page.on('download', () => downloads++)
  const db = await savedMedia(page, 'image', (route) => mode === 'failed'
    ? route.fulfill({ status: 503, json: { error: 'STORAGE_TIMEOUT' } })
    : mode === 'invalid' ? route.fulfill({ json: { error: 'unavailable' } })
      : route.fulfill({ contentType: 'image/png', body: readFileSync(fixturePath('image')) }))
  await expect(db.node.getByRole('alert')).toContainText('预览加载失败')
  await db.node.click({ position: { x: 90, y: 90 } })
  await expect(toolbar(page).getByRole('button', { name: '下载素材' })).toBeEnabled()
  await toolbar(page).getByRole('button', { name: '下载素材' }).click()
  await expect(page.locator('.download-error')).toContainText('503')
  expect(downloads).toBe(0)
  mode = 'invalid'
  await toolbar(page).getByRole('button', { name: '下载素材' }).click()
  await expect(page.locator('.download-error')).toContainText('不是有效的媒体文件')
  expect(downloads).toBe(0)
  mode = 'ready'
  const downloadEvent = page.waitForEvent('download')
  await toolbar(page).getByRole('button', { name: '下载素材' }).click()
  expect(readFileSync((await (await downloadEvent).path())!)).toEqual(db.bytes)
  await expect(page.locator('.download-error')).toHaveCount(0)
  await toolbar(page).getByRole('button', { name: '全屏预览', exact: true }).click()
  const preview = page.getByRole('dialog', { name: '图片全屏预览' })
  await expect(preview.getByRole('status')).toHaveCount(0)
  expect(await preview.locator('img').evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(960)
  await page.keyboard.press('Escape')
  await expect(db.node.getByRole('alert')).toContainText('预览加载失败')
  expect(db.writes.every((write) => write === 'POST /api/credits/quote' || write === `PUT /api/canvases/${canvasId}`)).toBe(true)
  expect(db.errors).toEqual([])
})
