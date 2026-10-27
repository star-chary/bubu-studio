// Explicit live acceptance: two logical Seedance 2.5 jobs, each 5s / 720p.
// A saved report must be resumed; never create replacement generation tasks.
import { chromium, expect } from '@playwright/test'
import { randomBytes, randomUUID } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'

if (process.env.RUN_LIVE_APIMART_VIDEO !== '1') throw new Error('Set RUN_LIVE_APIMART_VIDEO=1 for the two explicitly authorized live jobs.')
const base = 'http://127.0.0.1:5173'
const artifact = resolve(process.env.LIVE_ARTIFACT_DIR || 'artifacts/apimart-video-live')
await mkdir(artifact, { recursive: true })
const reportPath = resolve(artifact, 'live-report.json')
const sessionPath = resolve(artifact, 'live-session.json')
let report
try {
  report = JSON.parse(await readFile(reportPath, 'utf8'))
  if (process.env.RESUME_LIVE_APIMART_VIDEO !== '1') throw new Error('A report already exists. Resume with RESUME_LIVE_APIMART_VIDEO=1; do not create new jobs.')
} catch (error) {
  if (error.code !== 'ENOENT') throw error
  report = { canvasId: randomUUID(), sourceId: randomUUID(), nodeIds: { text: randomUUID(), reference: randomUUID() }, tasks: {}, checks: {}, pageErrors: [], verified: false }
}
let writes = Promise.resolve()
function persist() {
  writes = writes.then(() => writeFile(reportPath, JSON.stringify(report, null, 2)))
  return writes
}
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const context = await browser.newContext({ baseURL: base, viewport: { width: 1920, height: 1080 }, ...(report.initialized ? { storageState: sessionPath } : {}) })
const page = await context.newPage()
page.setDefaultTimeout(25_000)
page.on('pageerror', (error) => { report.pageErrors.push(error.message) })
page.on('request', (request) => {
  if (request.method() !== 'POST' || !request.url().endsWith('/api/videos/generations')) return
  const input = request.postDataJSON()
  assert.equal(input.provider, 'apimart')
  assert.equal(input.model, 'seedance-2.5')
  const mode = input.mode
  assert(['text', 'reference'].includes(mode))
  assert(!report.tasks[mode] || report.tasks[mode].id === input.taskId, 'Refusing a replacement generation task')
  report.tasks[mode] ??= { id: input.taskId, status: 'submitting' }
  void persist()
})
const node = (mode) => page.getByRole('article', { name: mode === 'text' ? '中转站文生验收' : '中转站图片参考验收', exact: true })
const editor = () => page.getByRole('textbox', { name: '视频提示词', exact: true })
async function json(response) {
  const value = await response.json()
  assert(response.ok(), `${response.status()} ${value.error?.code || ''}: ${value.error?.message || 'request failed'}`)
  return value
}
let session
async function write(path, data, method = 'POST') {
  return json(await page.request.fetch(path, { method, data, headers: { Origin: base, 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id } }))
}
async function submit(mode) {
  if (report.tasks[mode]) return
  await node(mode).click({ position: { x: 60, y: 70 } })
  if (mode === 'text') {
    await editor().fill('An origami paper boat drifts gently on calm blue water, subtle ripples, static camera, soft daylight, clean composition, no text.')
  } else {
    await page.getByRole('button', { name: '参考', exact: true }).click()
    await page.getByRole('article', { name: '验收参考图', exact: true }).click({ position: { x: 70, y: 70 } })
    await page.getByRole('button', { name: '返回节点', exact: true }).click()
    await editor().fill('参考 ')
    await editor().press('Control+End')
    await editor().pressSequentially('@')
    const menu = page.getByRole('dialog', { name: '素材引用菜单', exact: true })
    await menu.getByRole('button', { name: '图片素材', exact: true }).click()
    await menu.locator('.mention-submenu').getByRole('button', { name: '引用 验收参考图', exact: true }).click()
    await editor().pressSequentially(' 的几何形状与配色，制作干净的抽象动画，形状缓慢移动，光影轻微变化，固定镜头，不要文字。')
  }
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  const response = page.waitForResponse((result) => result.request().method() === 'POST' && result.url().endsWith('/api/videos/generations'))
  await page.getByRole('button', { name: '生成视频', exact: true }).click()
  const task = await json(await response)
  assert.equal(report.tasks[mode].id, task.id)
  report.tasks[mode] = { id: task.id, status: task.status }
  await persist()
  console.log(JSON.stringify({ mode, taskId: task.id, status: task.status }))
  await page.mouse.click(1050, 90)
}
async function waitForVideo(mode) {
  const id = report.tasks[mode]?.id
  assert(id, 'Missing saved task ID')
  const deadline = Date.now() + 25 * 60_000
  let previous = ''
  while (Date.now() < deadline) {
    const task = await json(await page.request.get(`/api/tasks/${id}`))
    report.tasks[mode] = { id, status: task.status, providerTaskId: task.providerTaskId, error: task.error, compiledPrompt: task.compiledPrompt, input: task.input, result: task.result }
    if (previous !== task.status) {
      previous = task.status
      await persist()
      console.log(JSON.stringify({ mode, taskId: id, status: task.status, error: task.error }))
    }
    if (task.status === 'succeeded') {
      assert.equal(task.input.provider, 'apimart')
      assert.equal(task.input.model, 'seedance-2.5')
      assert.equal(task.creditPoints, 0)
      assert.equal(task.creditStatus, 'legacy')
      assert.equal(task.result.asset.kind, 'videos')
      assert(task.result.durationSeconds > 0)
      return
    }
    if (['failed', 'interrupted', 'storage_failed'].includes(task.status)) throw new Error(`${mode}: ${task.error?.code} ${task.error?.message}; original task retained, no new generation submitted`)
    await new Promise((done) => setTimeout(done, 3000))
  }
  throw new Error(`${mode}: query timed out; keep task ${id}, do not resubmit`)
}
try {
  if (report.error) {
    report.previousErrors ??= []
    report.previousErrors.push(report.error)
    delete report.error
  }
  if (!report.initialized) {
    const email = `apimart-live-${randomUUID().slice(0, 8)}@example.test`
    const password = `Aa9!${randomBytes(9).toString('base64url')}`
    session = await json(await page.request.post('/api/auth/login', { data: { email, password }, headers: { Origin: base, 'X-Requested-With': 'frame-space' } }))
    await context.storageState({ path: sessionPath })
    report.userId = session.user.id
    await write('/api/canvases', { id: report.canvasId, title: 'APIMart 真实视频验收' })
    const selection = { provider: 'apimart', modelId: 'seedance-2.5', parameters: { size: '16:9', resolution: '720p', duration: 5, generate_audio: true } }
    const nodes = [
      { id: report.nodeIds.text, position: { x: 40, y: 150 }, data: { kind: 'video', name: '中转站文生验收', videoMode: 'text', modelSelection: structuredClone(selection) } },
      { id: report.nodeIds.reference, position: { x: 650, y: 150 }, data: { kind: 'video', name: '中转站图片参考验收', videoMode: 'reference', modelSelection: structuredClone(selection) } },
      { id: report.sourceId, position: { x: 1260, y: 150 }, data: { kind: 'image', name: '验收参考图', origin: 'upload' } },
    ]
    await write(`/api/canvases/${report.canvasId}`, { version: 0, snapshot: { nodes, viewport: { x: 0, y: 0, zoom: 1 } } }, 'PUT')
    const png = await readFile(new URL('../tests/fixtures/sample-image.png', import.meta.url))
    const asset = await json(await page.request.post(`/api/assets?canvasId=${report.canvasId}&nodeId=${report.sourceId}`, { data: png, headers: { Origin: base, 'Content-Type': 'application/octet-stream', 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id }, timeout: 90_000 }))
    assert((asset.asset || asset).videoReference.eligible)
    report.initialized = true
    await persist()
  } else session = await json(await page.request.get('/api/auth/me'))
  await page.goto(`/?canvas=${report.canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await expect(page.getByRole('article', { name: '验收参考图', exact: true })).toContainText('960 × 540')
  await submit('text')
  await submit('reference')
  await Promise.all([waitForVideo('text'), waitForVideo('reference')])
  for (const mode of ['text', 'reference']) {
    const video = node(mode).locator('video')
    await expect(video).toHaveJSProperty('readyState', 4, { timeout: 90_000 })
    await node(mode).getByRole('button', { name: '播放视频', exact: true }).click()
    await expect.poll(() => video.evaluate((element) => element.currentTime)).toBeGreaterThan(0)
    await node(mode).getByRole('button', { name: '暂停视频', exact: true }).click()
    report.checks[`${mode}Playback`] = true
  }
  await page.screenshot({ path: resolve(artifact, 'live-results.png') })
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.reload()
  for (const mode of ['text', 'reference']) await expect(node(mode).locator('video')).toHaveJSProperty('readyState', 4, { timeout: 90_000 })
  report.checks.refreshRestored = true
  report.verified = true
  report.verifiedAt = new Date().toISOString()
  assert.deepEqual(report.pageErrors, [])
  await page.screenshot({ path: resolve(artifact, 'live-restored.png') })
  await persist()
  console.log(JSON.stringify({ verified: report.verified, canvasId: report.canvasId, checks: report.checks, logicalGenerationTasks: Object.keys(report.tasks).length }))
} catch (error) {
  report.error = error.message
  await persist()
  await page.screenshot({ path: resolve(artifact, 'live-failure.png') }).catch(() => {})
  throw error
} finally {
  await writes
  await browser.close()
}
