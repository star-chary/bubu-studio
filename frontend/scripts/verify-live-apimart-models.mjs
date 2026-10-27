// Explicit one-job-per-model live acceptance. Resume saved IDs; never replace
// an uncertain submission. A slow model does not stop the remaining models.
import { chromium, expect } from '@playwright/test'
import { randomBytes, randomUUID } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'
import definitions from '../../backend/internal/apimart/video_models.json' with { type: 'json' }

if (process.env.RUN_LIVE_APIMART_MODELS !== '1') throw new Error('Set RUN_LIVE_APIMART_MODELS=1 for live model acceptance.')
const artifact = resolve(process.env.LIVE_ARTIFACT_DIR || 'artifacts/apimart-models-live')
await mkdir(artifact, { recursive: true })
const reportPath = resolve(artifact, 'models-report.json')
const sessionPath = resolve(artifact, 'private-session.json')
const baseURL = 'http://127.0.0.1:5173'
const waitLimit = 180_000
const terminal = new Set(['succeeded', 'failed', 'interrupted', 'storage_failed'])
const selectedModes = {
  'seedance-2.5': 'image', 'seedance-2.0-fast': 'reference', 'kling-v4': 'reference',
  'kling-3.0-turbo': 'image', 'kling-v3-omni': 'reference', 'wan3.0-video-prime': 'image',
  'MiniMax-H3': 'reference', 'MiniMax-H3-Max': 'image', 'happyhorse-1.0': 'image',
  'viduq4-preview': 'image', 'grok-imagine-video-1.5': 'reference', 'flux-3-video': 'image',
  'pixverse-v6': 'reference', 'veo3.1-fast-official': 'image', 'skyreels-v4-std': 'reference',
}
let report
try {
  report = JSON.parse(await readFile(reportPath, 'utf8'))
  assert.equal(process.env.RESUME_LIVE_APIMART_MODELS, '1', 'Saved report exists; explicitly resume it.')
} catch (error) {
  if (error.code !== 'ENOENT') throw error
  report = { canvasId: randomUUID(), sourceId: randomUUID(), startedAt: new Date().toISOString(), models: {}, pageErrors: [] }
}
let writes = Promise.resolve()
const persist = () => {
  const snapshot = JSON.stringify(report, null, 2)
  writes = writes.then(() => writeFile(reportPath, snapshot))
  return writes
}
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const context = await browser.newContext({ baseURL, viewport: { width: 1920, height: 1920 }, ...(report.initialized ? { storageState: sessionPath } : {}) })
const page = await context.newPage()
page.on('pageerror', (error) => report.pageErrors.push(error.message))
let session
async function json(response) {
  const data = await response.json()
  if (!response.ok()) throw new Error(`${response.status()} ${data.error?.code || 'HTTP_ERROR'} ${data.error?.message || ''}`)
  return data
}
async function write(url, data, method = 'POST') {
  return json(await context.request.fetch(url, { method, data, timeout: 75_000, headers: { Origin: baseURL, 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id } }))
}
function basicParameters(definition, mode) {
  const values = {}
  for (const field of definition.parameters) {
    let value = field.kind === 'fixed' ? field.fixed : field.default
    if (field.role === 'duration') value = field.enum ? Math.min(...field.enum) : field.minimum
    if (field.role === 'resolution') value = field.enum[0]
    if (field.role === 'ratio') value = field.enum.includes('16:9') ? '16:9' : field.enum[0]
    if (field.role === 'audio' && field.kind !== 'fixed') value = false
    values[field.key] = value
  }
  if (mode === 'image') Object.assign(values, definition.imageParameters)
  return values
}
function updateRecord(record, task) {
  Object.assign(record, { status: task.status, providerTaskId: task.providerTaskId, error: task.error, pollingError: task.pollingError, finishedAt: task.finishedAt, updatedAt: new Date().toISOString() })
  record.elapsedSeconds = Math.round(((task.finishedAt ? Date.parse(task.finishedAt) : Date.now()) - Date.parse(record.submittedAt)) / 1000)
  if (record.elapsedSeconds >= waitLimit / 1000) record.longDuration = true
  if (task.status === 'succeeded') {
    assert.equal(task.input.provider, 'apimart')
    assert.equal(task.creditPoints, 0)
    assert.equal(task.creditStatus, 'legacy')
    assert(task.result?.asset && task.result.durationSeconds > 0)
    record.result = { width: task.result.width, height: task.result.height, durationSeconds: task.result.durationSeconds, hasAudio: task.result.hasAudio, assetKey: task.result.asset.key }
  }
}
try {
  const models = definitions.filter((model) => model.generationEnabled)
  if (!report.initialized) {
    const email = `video-models-${randomUUID().slice(0, 8)}@example.test`
    const password = `Aa9!${randomBytes(9).toString('base64url')}`
    session = await json(await context.request.post('/api/auth/login', { data: { email, password }, headers: { Origin: baseURL, 'X-Requested-With': 'frame-space' } }))
    await context.storageState({ path: sessionPath })
    report.userId = session.user.id
    await write('/api/canvases', { id: report.canvasId, title: '中转站视频模型逐项验收' })
    const nodes = models.map((definition, index) => {
      const mode = selectedModes[definition.modelId] || 'text'
      assert(definition.modes.includes(mode))
      const nodeId = randomUUID()
      const parameters = basicParameters(definition, mode)
      report.models[definition.modelId] = { name: definition.name, nodeId, mode, parameters, status: 'not_submitted' }
      return { id: nodeId, position: { x: 40 + (index % 3) * 650, y: 70 + Math.floor(index / 3) * 420 }, data: {
        kind: 'video', name: `${definition.name} · ${mode}`, prompt: 'A small origami boat moves gently on calm water, fixed camera, soft daylight, no text.',
        videoMode: mode, modelSelection: { provider: 'apimart', modelId: definition.modelId, parameters },
        ...(mode !== 'text' ? { referenceIds: [report.sourceId] } : {}), ...(mode === 'image' ? { firstFrameId: report.sourceId } : {}),
      } }
    })
    nodes.push({ id: report.sourceId, position: { x: 1340, y: 3010 }, data: { kind: 'image', name: '合成参考图', origin: 'upload' } })
    await write(`/api/canvases/${report.canvasId}`, { version: 0, snapshot: { nodes, viewport: { x: 35, y: 50, zoom: .54 } } }, 'PUT')
    const png = await readFile(new URL('../tests/fixtures/sample-image.png', import.meta.url))
    const source = await json(await context.request.post(`/api/assets?canvasId=${report.canvasId}&nodeId=${report.sourceId}`, { data: png, timeout: 90_000, headers: { Origin: baseURL, 'Content-Type': 'application/octet-stream', 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id } }))
    report.sourceAssetKey = (source.asset || source).key
    report.initialized = true
    await persist()
  } else session = await json(await context.request.get('/api/auth/me'))
  async function verifyModel(definition) {
    const record = report.models[definition.modelId]
    if (terminal.has(record.status)) return
    if (!record.taskId) {
      record.taskId = randomUUID()
      record.submittedAt = new Date().toISOString()
      record.status = 'submission_started'
      await persist() // Durable ID before the one allowed POST.
      const input = { taskId: record.taskId, provider: 'apimart', model: definition.modelId, mode: record.mode, canvasId: report.canvasId, nodeId: record.nodeId,
        prompt: record.mode === 'text' ? 'A small origami boat moves gently on calm water, fixed camera, soft daylight, no text.' : 'Use the clean geometric shapes and colors from the image. Gentle motion, fixed camera, no new text.', parameters: record.parameters,
        ...(record.mode !== 'text' ? { references: [{ nodeId: report.sourceId, assetKey: report.sourceAssetKey, kind: 'image', ...(record.mode === 'image' ? { role: 'first_frame' } : {}) }] } : {}),
      }
      try { updateRecord(record, await write('/api/videos/generations', input)) }
      catch (error) { record.requestError = error.message; record.status = 'submission_unconfirmed'; await persist(); console.log(JSON.stringify({ model: definition.modelId, status: record.status, error: record.requestError })); return }
      await persist()
    }
    const started = Date.now()
    let lastStatus = ''
    let queried = false
    while (!queried || Date.now() - Date.parse(record.submittedAt) < waitLimit) {
      queried = true
      try {
        const task = await json(await context.request.get(`/api/tasks/${record.taskId}`, { timeout: 20_000 }))
        updateRecord(record, task)
        if (record.status !== lastStatus) {
          lastStatus = record.status
          console.log(JSON.stringify({ model: definition.modelId, mode: record.mode, taskId: record.taskId, status: record.status, error: record.error }))
        }
        await persist()
        if (terminal.has(record.status)) break
      } catch (error) { record.lastQueryError = error.message }
      await new Promise((done) => setTimeout(done, 5000))
    }
    record.waitSeconds = Math.round((Date.now() - started) / 1000)
    if (!terminal.has(record.status)) {
      record.delayed = true
      console.log(JSON.stringify({ model: definition.modelId, status: record.status, delayed: true, action: 'kept original task; continuing with next model' }))
    }
    await persist()
  }
  const remaining = [...models]
  await Promise.all(Array.from({ length: 3 }, async () => {
    while (remaining.length) await verifyModel(remaining.shift())
  }))
  // Revisit accepted slow jobs once, without restarting their generation.
  for (const record of Object.values(report.models)) {
    if (!record.taskId || terminal.has(record.status)) continue
    try { updateRecord(record, await json(await context.request.get(`/api/tasks/${record.taskId}`, { timeout: 20_000 }))) }
    catch (error) { record.lastQueryError = error.message }
    await persist()
  }
  await page.goto(`/?canvas=${report.canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存', { timeout: 30_000 })
  for (const record of Object.values(report.models)) {
    if (record.status !== 'succeeded') continue
    try {
      const video = page.getByRole('article', { name: `${record.name} · ${record.mode}`, exact: true }).locator('video')
      await expect.poll(() => video.evaluate((element) => element.readyState), { timeout: 30_000 }).toBeGreaterThanOrEqual(2)
      record.frontendRestored = true
      record.playback = await video.evaluate(async (element) => {
        element.muted = true; await element.play()
        await new Promise((done) => setTimeout(done, 350))
        const played = element.currentTime > 0; element.pause(); return played
      })
    } catch (error) { record.playbackError = error.message.slice(0, 250) }
  }
  await page.screenshot({ path: resolve(artifact, 'models-restored.png') })
  report.finishedAt = new Date().toISOString()
  await persist()
  console.log(JSON.stringify({ finished: true, counts: Object.values(report.models).reduce((out, row) => ({ ...out, [row.status]: (out[row.status] || 0) + 1 }), {}), delayed: Object.values(report.models).filter((row) => row.delayed).length, pageErrors: report.pageErrors }))
} finally {
  await persist()
  await browser.close()
}
