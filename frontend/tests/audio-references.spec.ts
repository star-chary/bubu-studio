import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'

test.use({ viewport: { width: 1680, height: 1000 } })
const fixture = (name: string) => fileURLToPath(new URL(`./fixtures/${name}`, import.meta.url))
const article = (page: Page, name: string) => page.getByRole('article', { name, exact: true })
async function upload(page: Page, files: Parameters<import('@playwright/test').FileChooser['setFiles']>[0], x = 70, y = 130) {
  await page.mouse.click(x, y, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(files)
}
async function addNode(page: Page, kind: '图片' | '视频') {
  await page.mouse.click(1120, 140, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
  await article(page, `${kind}节点 1`).click({ position: { x: 60, y: 70 } })
}
function assetFor(canvasId: string, nodeId: string, body: Buffer) {
  const audio = body.toString('ascii', 0, 4) === 'RIFF'
  const image = body[0] === 137
  const kind = audio ? 'audios' : image ? 'images' : 'videos'
  const key = `test/${canvasId}/${nodeId}`
  return { key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, canvasId, nodeId, kind, source: 'uploads', bytes: body.length,
    contentType: audio ? 'audio/wav' : image ? 'image/png' : 'video/mp4',
    media: image ? { width: 960, height: 540 } : audio ? { durationSeconds: 2, audioCodec: 'pcm_s16le' } : { width: 854, height: 480, durationSeconds: 2, frameRate: 24, videoCodec: 'h264' },
    videoReference: { eligible: true } }
}
async function enabledStorage(page: Page) {
  await page.route('**/api/storage/config', (route) => route.fulfill({ json: { enabled: true, maxImageBytes: 20 << 20, maxVideoBytes: 200 << 20, maxAudioBytes: 15_000_000 } }))
}

test('WAV 和 MP3 能播放与拖动进度；音频没有提示词入口且不能被图片引用', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, [fixture('reference-audio.wav'), fixture('reference-audio.mp3')])
  for (const name of ['reference-audio.wav', 'reference-audio.mp3']) {
    const node = article(page, name)
    await expect(node.getByRole('button', { name: '播放音频' })).toBeVisible()
    const box = await node.boundingBox()
    await node.getByRole('button', { name: '播放音频' }).click()
    await expect(node.getByRole('button', { name: '暂停音频' })).toBeVisible()
    await node.getByRole('button', { name: '暂停音频' }).click()
    await node.getByRole('slider', { name: '音频进度' }).fill('1')
    await expect.poll(() => node.locator('audio').evaluate((el: HTMLAudioElement) => el.currentTime)).toBeCloseTo(1, 1)
    expect(await node.boundingBox()).toEqual(box)
    await node.click({ position: { x: 80, y: 60 } })
    await expect(page.getByRole('dialog')).toHaveCount(0)
  }
  await addNode(page, '图片')
  await page.getByRole('button', { name: '参考', exact: true }).click()
  await article(page, 'reference-audio.wav').click({ position: { x: 80, y: 60 } })
  await expect(page.getByLabel('选择参考', { exact: true })).toContainText('图片节点只能参考图片')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('audio-playback.png') })
  expect(errors).toEqual([])
})

test('保存后按图片1、视频1、图片2、音频1编号；播放不误选、移除同步连线和编号', async ({ page }, testInfo) => {
  await enabledStorage(page)
  let uploads = 0
  let generations = 0
  page.on('request', (r) => { if (r.url().includes('/generations')) generations++ })
  await page.route('**/api/assets?*', async (route) => {
    uploads++
    const q = new URL(route.request().url()).searchParams
    await route.fulfill({ status: 201, json: assetFor(q.get('canvasId')!, q.get('nodeId')!, route.request().postDataBuffer()!) })
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, [
    { name: 'sample-image.png', mimeType: 'image/png', buffer: readFileSync(fixture('sample-image.png')) },
    { name: 'reference-video.mp4', mimeType: 'video/mp4', buffer: readFileSync(fixture('reference-video.mp4')) },
    { name: 'second.png', mimeType: 'image/png', buffer: readFileSync(fixture('sample-image.png')) },
    { name: 'reference-audio.wav', mimeType: 'audio/wav', buffer: readFileSync(fixture('reference-audio.wav')) },
  ])
  await expect.poll(() => uploads).toBe(4)
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(0)
  await addNode(page, '视频')
  await page.getByRole('textbox').fill('参考图片1、视频1、图片2和音频1')
  await page.getByRole('button', { name: '参考', exact: true }).click()
  const audio = article(page, 'reference-audio.wav')
  await audio.getByRole('button', { name: '播放音频' }).click()
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await audio.getByRole('button', { name: '暂停音频' }).click()
  for (const name of ['sample-image.png', 'reference-video.mp4', 'second.png', 'reference-audio.wav']) await article(page, name).click({ position: { x: 70, y: 60 } })
  const panel = page.getByRole('dialog')
  await expect(panel.locator('.reference-index')).toHaveText(['图片1', '视频1', '图片2', '音频1'])
  await expect(panel).toContainText('参考素材检查通过')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(4)
  await expect(panel.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled() // 仍在参考选择中
  await page.screenshot({ path: testInfo.outputPath('mixed-references.png') })
  await panel.getByRole('button', { name: '移除参考 sample-image.png' }).click()
  await expect(panel.locator('.reference-index')).toHaveText(['视频1', '图片1', '音频1'])
  await expect(page.locator('.vue-flow__edge')).toHaveCount(3)
  // A saved source can recover after a temporary read failure; do not silently
  // delete its durable reference or renumber other references during that failure.
  await audio.locator('audio').dispatchEvent('error', { bubbles: false })
  await expect(panel.locator('.reference-index')).toHaveText(['视频1', '图片1', '音频1'])
  await expect(panel).toContainText('音频1：素材正在加载、生成或无法预览')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(3)
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 375, height: 812 })
  const box = await panel.boundingBox()
  expect(box!.x).toBeGreaterThanOrEqual(16)
  expect(box!.x + box!.width).toBeLessThanOrEqual(359)
  expect(generations).toBe(0)
})

test('音频保存失败保留预览，可重试；服务端不合格原因显示在参考列表', async ({ page }) => {
  await enabledStorage(page)
  let calls = 0
  await page.route('**/api/assets?*', async (route) => {
    if (++calls === 1) return route.fulfill({ status: 503, json: { error: { message: '服务端尚未配置 ffprobe' } } })
    const q = new URL(route.request().url()).searchParams
    const asset = assetFor(q.get('canvasId')!, q.get('nodeId')!, route.request().postDataBuffer()!)
    await route.fulfill({ status: 201, json: { ...asset, videoReference: { eligible: false, reason: '单个参考音视频时长须为 2～30 秒' } } })
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, fixture('reference-audio.wav'))
  const audio = article(page, 'reference-audio.wav')
  await expect(audio.getByRole('alert')).toContainText('ffprobe')
  await expect(audio.locator('audio')).toHaveAttribute('src', /^blob:/)
  await audio.getByRole('button', { name: '重新保存' }).click()
  await expect(audio.getByRole('alert')).toHaveCount(0)
  await addNode(page, '视频')
  await page.getByRole('button', { name: '参考', exact: true }).click()
  await audio.click({ position: { x: 70, y: 60 } })
  await expect(page.getByRole('dialog')).toContainText('音频1：单个参考音视频时长须为 2～30 秒')
  await expect(page.getByRole('dialog').locator('.prompt-reference')).toHaveClass(/has-error/)
  expect(calls).toBe(2)
})
