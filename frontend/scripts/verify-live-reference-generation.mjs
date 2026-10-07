// Explicit paid acceptance: one image and one 4s/480p video, synthetic references.
// A rerun without RESUME_REFERENCE_GENERATION=1 creates NEW paid tasks.
import { chromium, expect } from '@playwright/test'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { randomUUID } from 'node:crypto'
import assert from 'node:assert/strict'

if (process.env.RUN_LIVE_REFERENCE_GENERATION !== '1') throw new Error('Explicit RUN_LIVE_REFERENCE_GENERATION=1 is required.')
const base = 'http://127.0.0.1:5173'
const artifact = 'artifacts/reference-generation-live'
await mkdir('artifacts', { recursive: true })
const resume = process.env.RESUME_REFERENCE_GENERATION === '1'
const report = resume ? JSON.parse(await readFile(`${artifact}.json`, 'utf8')) : { canvasId: randomUUID(), ids: Array.from({ length: 5 }, randomUUID), tasks: {}, errors: [], verified: false }
const [imageId, videoId, audioId, imageTarget, videoTarget] = report.ids
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL: base, viewport: { width: 1680, height: 1100 } })
page.setDefaultTimeout(20000)
page.on('pageerror', (error) => report.errors.push(error.message))
const persist = () => writeFile(`${artifact}.json`, JSON.stringify(report, null, 2))
const article = (name) => page.getByRole('article', { name, exact: true })
const editor = () => page.getByRole('textbox', { name: /^(图片|视频)提示词$/ })
async function json(response) { const body = await response.json(); assert(response.ok(), `${response.status()} ${body.error?.code}: ${body.error?.message}`); return body }
async function mention(name, kind) {
  await editor().press('Control+End'); await editor().pressSequentially('@')
  const popup = page.getByRole('dialog', { name: '素材引用菜单', exact: true })
  await popup.getByRole('button', { name: `${kind}素材`, exact: true }).click()
  await popup.locator('.mention-submenu').getByRole('button', { name: `引用 ${name}`, exact: true }).click()
}
async function submit(kind) {
  assert(!report.tasks[kind], 'Refusing to create a second task of this kind')
  const response = page.waitForResponse((r) => r.url().endsWith(`/api/${kind}s/generations`))
  await page.getByRole('button', { name: kind === 'image' ? '生成图片' : '生成视频', exact: true }).click()
  const task = await json(await response)
  report.tasks[kind] = task
  await persist()
  console.log(JSON.stringify({ kind, taskId: task.id, status: task.status, compiledPrompt: task.compiledPrompt }))
}
async function wait(kind) {
  const id = report.tasks[kind].id
  const deadline = Date.now() + (kind === 'video' ? 25 * 60_000 : 6 * 60_000)
  let previous = ''
  while (Date.now() < deadline) {
    const task = await json(await page.request.get(`/api/tasks/${id}`))
    report.tasks[kind] = task
    if (task.status !== previous) { previous = task.status; await persist(); console.log(JSON.stringify({ kind, taskId: id, status: task.status, error: task.error })) }
    if (task.status === 'succeeded') { assert(task.result?.asset); assert(!task.result.storageError); return task }
    if (['failed', 'interrupted', 'storage_failed'].includes(task.status)) throw new Error(`${kind}: ${task.error?.code} / ${task.error?.message}; no new generation submitted`)
    await new Promise((resolve) => setTimeout(resolve, 3000))
  }
  throw new Error(`Query timed out; retain task ${id}, never submit it again`)
}
try {
  if (!resume) {
    await json(await page.request.post('/api/canvases', { data: { id: report.canvasId } }))
    const nodes = [
      { id: imageId, position: { x: 40, y: 145 }, data: { kind: 'image', name: 'sample-image.png', origin: 'upload' } },
      { id: videoId, position: { x: 545, y: 145 }, data: { kind: 'video', name: 'reference-video.mp4', origin: 'upload' } },
      { id: audioId, position: { x: 40, y: 540 }, data: { kind: 'audio', name: 'reference-audio.wav', origin: 'upload' } },
      { id: imageTarget, position: { x: 1080, y: 145 }, data: { kind: 'image', name: '图片引用验收' } },
      { id: videoTarget, position: { x: 1080, y: 590 }, data: { kind: 'video', name: '视频引用验收' } },
    ]
    await json(await page.request.put(`/api/canvases/${report.canvasId}`, { data: { version: 0, snapshot: { nodes, viewport: { x: 0, y: 0, zoom: 1 } } } }))
    for (const [id, name] of [[imageId, 'sample-image.png'], [videoId, 'reference-video.mp4'], [audioId, 'reference-audio.wav']]) {
      const asset = await json(await page.request.post(`/api/assets?canvasId=${report.canvasId}&nodeId=${id}`, { data: await readFile(new URL(`../tests/fixtures/${name}`, import.meta.url)), headers: { 'Content-Type': 'application/octet-stream' }, timeout: 90000 }))
      assert(asset.videoReference?.eligible ?? asset.asset?.videoReference?.eligible)
    }
    await persist()
  }
  await page.goto(`/?canvas=${report.canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await expect(article('sample-image.png')).toContainText('960 × 540')
  if (!report.tasks.image) {
    await article('图片引用验收').click({ position: { x: 60, y: 70 } })
    await editor().fill('参考 '); await mention('sample-image.png', '图片')
    await editor().pressSequentially(' 的彩色几何形状，生成一张干净的抽象海报，保持 '); await mention('sample-image.png', '图片')
    await editor().pressSequentially(' 的配色，不要文字。')
    await submit('image')
  }
  const imageTask = await wait('image')
  assert.equal(imageTask.bindings.length, 1)
  assert.equal((imageTask.compiledPrompt.match(/图片1/g) ?? []).length, 2)
  await page.mouse.click(820, 100)
  if (!report.tasks.video) {
    await article('视频引用验收').click({ position: { x: 60, y: 70 } })
    await editor().fill('参考 '); await mention('sample-image.png', '图片')
    await editor().pressSequentially(' 的彩色几何图案、'); await mention('reference-video.mp4', '视频')
    await editor().pressSequentially(' 的图形运动和音频1的节奏，生成一段全新的4秒抽象动画。镜头固定，运动平滑，不要字幕。')
    await page.getByRole('button', { name: '参考', exact: true }).click()
    await article('reference-audio.wav').click({ position: { x: 55, y: 50 } })
    await page.getByRole('button', { name: '结束参考选择', exact: true }).click()
    await page.getByRole('button', { name: /^视频参数，/ }).click()
    await expect(page.getByRole('radio', { name: '480P', exact: true })).toBeChecked()
    await expect(page.getByRole('slider', { name: '视频时长', exact: true })).toHaveValue('4')
    await page.keyboard.press('Escape')
    await submit('video')
  }
  const task = await wait('video')
  assert.deepEqual(task.bindings.map((r) => r.kind), ['image', 'video', 'audio'])
  assert.equal(task.result.asset.kind, 'videos')
  assert(Math.abs(task.result.durationSeconds - 4) <= .5)
  assert.equal(task.result.hasAudio, true)
  await page.reload()
  const video = article('视频引用验收').locator('video')
  await expect(video).toBeVisible({ timeout: 30000 })
  await expect.poll(() => video.evaluate((v) => v.readyState)).toBeGreaterThanOrEqual(2)
  const playback = await video.evaluate(async (v) => { v.muted = true; await v.play(); await new Promise((r) => setTimeout(r, 600)); const value = { width: v.videoWidth, height: v.videoHeight, duration: v.duration, currentTime: v.currentTime }; v.pause(); return value })
  assert(playback.currentTime > 0)
  const range = await page.request.get(task.result.url, { headers: { Range: 'bytes=0-1023' } })
  assert.equal(range.status(), 206)
  assert.equal((await range.body()).length, 1024)
  report.playback = playback; report.range = range.status(); report.verified = true; delete report.failure; report.verifiedAt = new Date().toISOString()
  await page.screenshot({ path: `${artifact}.png` })
  console.log(JSON.stringify({ verified: true, canvasId: report.canvasId, playback, errors: report.errors }))
} catch (error) { report.failure = error.message; console.error(error.message); process.exitCode = 1 }
finally { await persist(); await browser.close() }
