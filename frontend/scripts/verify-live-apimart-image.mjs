// Exactly two separately authorized jobs: one text image, then one image edit.
// The outgoing task ID is persisted before the request leaves the browser.
import { chromium, expect } from '@playwright/test'
import { randomBytes, randomUUID } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'

if (process.env.RUN_LIVE_APIMART_IMAGE !== '1') throw new Error('Explicit authorization required: set RUN_LIVE_APIMART_IMAGE=1.')
const base = 'http://127.0.0.1:5173'
const artifact = resolve(process.env.LIVE_IMAGE_ARTIFACT_DIR || 'artifacts/apimart-image-live')
await mkdir(artifact, { recursive: true })
const reportPath = resolve(artifact, 'live-report.json')
const sessionPath = resolve(artifact, 'live-session.json')
let report
try {
  report = JSON.parse(await readFile(reportPath, 'utf8'))
  if (process.env.RESUME_LIVE_APIMART_IMAGE !== '1') throw new Error('A report already exists; resume only, never create replacement tasks.')
} catch (error) {
  if (error.code !== 'ENOENT') throw error
  report = { canvasId: randomUUID(), sourceId: randomUUID(), nodeIds: { text: randomUUID(), reference: randomUUID() }, tasks: {}, checks: {}, pageErrors: [], verified: false }
}
let writes = Promise.resolve()
function persist() {
  writes = writes.then(() => writeFile(reportPath, JSON.stringify(report, null, 2)))
  return writes
}
await persist()
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const context = await browser.newContext({ baseURL: base, viewport: { width: 1920, height: 1080 }, ...(report.initialized ? { storageState: sessionPath } : {}) })
const page = await context.newPage()
page.setDefaultTimeout(25_000)
page.on('pageerror', (error) => { report.pageErrors.push(error.message) })
await context.route('**/api/images/generations', async (route) => {
  if (route.request().method() !== 'POST') return route.continue()
  const input = route.request().postDataJSON()
  const mode = Object.entries(report.nodeIds).find(([, id]) => id === input.nodeId)?.[0]
  if (!mode || report.tasks[mode] || input.provider !== 'apimart' || input.model !== 'seedream-5-0-lite' || input.parameters?.n !== 1 || input.parameters?.size !== '1:1' || input.parameters?.resolution !== '2K' || 'acceptedPoints' in input || (mode === 'text' && input.referenceKeys?.length) || (mode === 'reference' && input.referenceKeys?.length !== 1)) {
    await route.abort()
    throw new Error('Blocked unexpected or repeated live image submission.')
  }
  report.tasks[mode] = { id: input.taskId, status: 'submitting' }
  await persist()
  await route.continue()
})
const node = (mode) => page.getByRole('article', { name: mode === 'text' ? '文生图验收' : '图生图验收', exact: true })
const editor = () => page.getByRole('textbox', { name: '图片提示词', exact: true })
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
  if (mode === 'reference') {
    await page.getByRole('button', { name: '参考', exact: true }).click()
    await page.getByRole('article', { name: '合成验收参考图', exact: true }).click({ position: { x: 70, y: 70 } })
    await page.getByRole('button', { name: '返回节点', exact: true }).click()
  }
  await editor().fill(mode === 'text'
    ? 'A simple white paper boat floating on a calm blue lake, soft daylight, clean composition, no text.'
    : 'Use the reference image as the composition, turn the geometric shapes into a clean watercolor illustration, no text.')
  const button = page.getByRole('button', { name: '生成图片', exact: true })
  await expect(button).toBeEnabled()
  const response = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/api/images/generations'))
  await button.click()
  const task = await json(await response)
  assert.equal(task.id, report.tasks[mode].id)
  report.tasks[mode].status = task.status
  await persist()
}
async function waitForImage(mode) {
  const id = report.tasks[mode]?.id
  assert(id, 'Missing saved task ID')
  const deadline = Date.now() + 20 * 60_000
  while (Date.now() < deadline) {
    const task = await json(await page.request.get(`/api/tasks/${id}`))
    report.tasks[mode] = { id, status: task.status, providerTaskId: task.providerTaskId, error: task.error }
    await persist()
    if (task.status === 'succeeded') {
      assert.equal(task.input.provider, 'apimart')
      assert.equal(task.input.model, 'seedream-5-0-lite')
      assert.equal(task.creditPoints, 0)
      assert.equal(task.creditStatus, 'legacy')
      assert.equal(task.result?.asset?.kind, 'images', 'Image must be saved to OSS')
      assert(task.result?.url?.startsWith('/api/assets/content?'))
      return
    }
    if (['failed', 'interrupted', 'storage_failed'].includes(task.status)) throw new Error(`${mode}: ${task.error?.code || task.status}; do not create a replacement task`)
    await new Promise((done) => setTimeout(done, 3000))
  }
  throw new Error(`${mode}: query timed out; preserve task ${id} and do not resubmit`)
}
try {
  if (!report.initialized) {
    if (report.userId) throw new Error('Setup was interrupted; inspect the existing account and canvas before resuming.')
    const email = `apimart-image-${randomUUID().slice(0, 8)}@example.test`
    const password = `Aa9!${randomBytes(9).toString('base64url')}`
    session = await json(await page.request.post('/api/auth/login', { data: { email, password }, headers: { Origin: base, 'X-Requested-With': 'frame-space' } }))
    await context.storageState({ path: sessionPath })
    report.userId = session.user.id
    await persist()
    await write('/api/canvases', { id: report.canvasId, title: 'APIMart 真实图片验收' })
    const selection = { provider: 'apimart', modelId: 'seedream-5-0-lite', parameters: { size: '1:1', resolution: '2K', n: 1 } }
    const nodes = [
      { id: report.nodeIds.text, position: { x: 40, y: 150 }, data: { kind: 'image', name: '文生图验收', modelSelection: structuredClone(selection) } },
      { id: report.nodeIds.reference, position: { x: 650, y: 150 }, data: { kind: 'image', name: '图生图验收', modelSelection: structuredClone(selection) } },
      { id: report.sourceId, position: { x: 1260, y: 150 }, data: { kind: 'image', name: '合成验收参考图', origin: 'upload' } },
    ]
    await write(`/api/canvases/${report.canvasId}`, { version: 0, snapshot: { nodes, viewport: { x: 0, y: 0, zoom: 1 } } }, 'PUT')
    const png = await readFile(new URL('../tests/fixtures/sample-image.png', import.meta.url))
    await json(await page.request.post(`/api/assets?canvasId=${report.canvasId}&nodeId=${report.sourceId}`, { data: png, headers: { Origin: base, 'Content-Type': 'application/octet-stream', 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id }, timeout: 90_000 }))
    report.initialized = true
    await persist()
  } else session = await json(await page.request.get('/api/auth/me'))
  await page.goto(`/?canvas=${report.canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  for (const mode of ['text', 'reference']) {
    await submit(mode)
    await waitForImage(mode)
    const picture = node(mode).locator('img')
    await expect(picture).toBeVisible({ timeout: 90_000 })
    await expect.poll(() => picture.evaluate((img) => img.complete && img.naturalWidth > 0)).toBe(true)
    report.checks[`${mode}Displayed`] = true
    await persist()
  }
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.reload()
  for (const mode of ['text', 'reference']) await expect(node(mode).locator('img')).toBeVisible({ timeout: 90_000 })
  report.checks.refreshRestored = true
  assert.deepEqual(report.pageErrors, [])
  report.verified = true
  report.verifiedAt = new Date().toISOString()
  await page.screenshot({ path: resolve(artifact, 'live-results.png') })
  await persist()
  console.log(JSON.stringify({ verified: true, canvasId: report.canvasId, tasks: report.tasks, checks: report.checks }))
} catch (error) {
  report.error = error.message
  await persist()
  await page.screenshot({ path: resolve(artifact, 'live-failure.png') }).catch(() => {})
  throw error
} finally {
  await writes
  await browser.close()
}
