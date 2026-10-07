// Real browser -> Go -> PostgreSQL + OSS. Only synthetic fixtures are uploaded.
// This stage verifies media/reference capability; it never creates a model task.
import { chromium, expect } from '@playwright/test'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import assert from 'node:assert/strict'

if (process.env.RUN_LIVE_MEDIA_REFERENCES !== '1') throw new Error('Set RUN_LIVE_MEDIA_REFERENCES=1 to upload synthetic fixtures to the configured OSS bucket.')
const baseURL = 'http://127.0.0.1:5173'
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL, viewport: { width: 1680, height: 1000 } })
page.setDefaultTimeout(15000)
const names = ['sample-image.png', 'reference-video.mp4', 'reference-audio.wav', 'reference-audio.mp3']
const files = names.map((name) => fileURLToPath(new URL(`../tests/fixtures/${name}`, import.meta.url)))
const errors = []
let generationRequests = 0
page.on('pageerror', (error) => errors.push(error.message))
// Capture a response clone as the app reads it; Chrome can evict response bodies
// from its inspector cache during uploads. This does not alter the API response.
await page.addInitScript(() => {
  window.__mediaUploads = []
  const originalFetch = window.fetch
  window.fetch = async function (...args) {
    const response = await originalFetch.apply(this, args)
    if (new URL(response.url).pathname === '/api/assets') window.__mediaUploads.push({ status: response.status, asset: await response.clone().json() })
    return response
  }
})
await page.route('**/api/*/generations', (route) => { generationRequests++; return route.abort() })
const article = (name) => page.getByRole('article', { name, exact: true })
const saved = () => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存', { timeout: 20000 })
try {
  await page.goto('/')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await saved()
  const canvasId = new URL(page.url()).searchParams.get('canvas')
  assert(canvasId)
  await page.mouse.click(70, 130, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(files)
  await expect.poll(() => page.evaluate(() => window.__mediaUploads.length), { timeout: 135000 }).toBe(4)
  const uploads = await page.evaluate(() => window.__mediaUploads)
  assert(uploads.every((r) => r.status === 201), `Upload failed: ${JSON.stringify(uploads.map((r) => ({ status: r.status, error: r.asset.error })))}`)
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(0)
  const assets = uploads.map((r) => r.asset)
  for (const asset of assets) assert.equal(asset.videoReference?.eligible, true)
  await page.mouse.click(1120, 140, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '视频', exact: true }).click()
  const target = article('视频节点 1')
  await target.click({ position: { x: 60, y: 70 } })
  const prompt = '参考图片1的色彩、视频1的运动，以及音频1和音频2的节奏，生成 4 秒视频。'
  await page.getByRole('textbox').fill(prompt)
  await page.getByRole('button', { name: '参考', exact: true }).click()
  for (const name of names) await article(name).click({ position: { x: 70, y: 60 } })
  await expect(page.getByRole('dialog').locator('.reference-index')).toHaveText(['图片1', '视频1', '音频1', '音频2'])
  await expect(page.getByRole('dialog')).toContainText('参考素材检查通过')
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved()
  const canvas = await page.request.get(`/api/canvases/${canvasId}`).then((r) => r.json())
  assert.equal(canvas.assets.length, 4)
  assert.equal(canvas.tasks.length, 0)
  const targetSnapshot = canvas.snapshot.nodes.find((n) => n.data.kind === 'video' && n.data.origin !== 'upload')
  assert.equal(targetSnapshot.data.referenceIds.length, 4)
  const checks = []
  for (let i = 0; i < names.length; i++) {
    const node = canvas.snapshot.nodes.find((n) => n.data.name === names[i])
    const asset = canvas.assets.find((a) => a.nodeId === node.id)
    const original = await readFile(files[i])
    const response = await page.request.get(asset.url)
    assert(response.ok())
    const digest = (bytes) => createHash('sha256').update(bytes).digest('hex')
    assert.equal(digest(await response.body()), digest(original))
    if (asset.kind !== 'images') {
      const partial = await page.request.get(asset.url, { headers: { Range: 'bytes=0-31' } })
      assert.equal(partial.status(), 206)
      assert((await partial.body()).equals(original.subarray(0, 32)))
    }
    checks.push({ name: names[i], nodeId: node.id, key: asset.key, bytes: asset.bytes, kind: asset.kind, media: asset.media, sha256: digest(original), contentVerified: true, rangeVerified: asset.kind !== 'images' })
  }
  await page.reload()
  await saved()
  await expect(page.locator('.vue-flow__edge')).toHaveCount(4)
  for (const name of names.slice(1)) {
    const type = name.endsWith('.mp4') ? '视频' : '音频'
    const node = article(name)
    await node.getByRole('button', { name: `播放${type}`, exact: true }).click()
    await expect(node.getByRole('button', { name: `暂停${type}`, exact: true })).toBeVisible()
    await node.getByRole('button', { name: `暂停${type}`, exact: true }).click()
    await node.getByRole('slider').fill('1')
  }
  await target.click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox')).toHaveText(prompt)
  await expect(page.getByRole('dialog').locator('.reference-index')).toHaveText(['图片1', '视频1', '音频1', '音频2'])
  await expect(page.getByRole('dialog')).toContainText('参考素材检查通过')
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: 'artifacts/media-references-live.png', fullPage: true })
  assert.equal(generationRequests, 0)
  assert.deepEqual(errors, [])
  const evidence = { verifiedAt: new Date().toISOString(), canvasId, canvasURL: page.url(), uploadCount: uploads.length, generationRequests, paidModelCalls: 0, databaseRestored: true, referencesRestored: true, playbackAfterReload: true, fixtures: checks, pageErrors: errors, screenshot: 'artifacts/media-references-live.png' }
  await writeFile('artifacts/media-references-live.json', JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify({ canvasURL: evidence.canvasURL, uploads: uploads.length, referencesRestored: true, playbackAfterReload: true, paidModelCalls: 0, pageErrors: errors }))
} catch (error) {
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: 'artifacts/media-references-live-failure.png', fullPage: true }).catch(() => {})
  throw error
} finally { await browser.close() }
