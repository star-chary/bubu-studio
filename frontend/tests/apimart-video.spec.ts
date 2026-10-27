import { readFileSync } from 'node:fs'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { GenerationTask, SavedCanvas, TaskInput } from '../src/api/persistence'

const canvasID = '11111111-1111-4111-8111-111111111111'
const targetID = '22222222-2222-4222-8222-222222222222'
const sourceID = '33333333-3333-4333-8333-333333333333'
const editor = (page: Page) => page.getByRole('textbox', { name: '视频提示词', exact: true })
const generate = (page: Page) => page.getByRole('button', { name: '生成视频', exact: true })

async function setup(page: Page, reference?: 'image' | 'audio') {
  const png = readFileSync(new URL('./fixtures/sample-image.png', import.meta.url))
  const mp4 = readFileSync(new URL('./fixtures/reference-video.mp4', import.meta.url))
  const canvas: SavedCanvas = { id: canvasID, version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [], snapshot: {
    viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: targetID, position: { x: 80, y: 130 }, data: {
      kind: 'video', name: '中转站视频', prompt: '', videoMode: 'reference', referenceIds: reference ? [sourceID] : [],
      modelSelection: { provider: 'apimart', modelId: 'seedance-2.5', parameters: { size: '16:9', resolution: '720p', duration: 5, generate_audio: false, seed: 42 } },
    } }],
  } }
  if (reference) {
    canvas.snapshot.nodes.push({ id: sourceID, position: { x: 800, y: 130 }, data: { kind: reference, name: '参考图片', origin: 'upload' } })
    canvas.assets.push({ key: 'source', url: '/api/assets/content?key=source', canvasId: canvasID, nodeId: sourceID, kind: reference === 'image' ? 'images' : 'audios', source: 'uploads', contentType: reference === 'image' ? 'image/png' : 'audio/wav', bytes: png.length, media: reference === 'image' ? { width: 960, height: 540 } : { durationSeconds: 2 }, videoReference: { eligible: true } })
  }
  const submissions: TaskInput[] = []
  let quotes = 0
  await page.route('**/api/credits', (route) => route.fulfill({ status: 503, json: { error: { message: '余额服务不可用' } } }))
  await page.route('**/api/credits/quote', (route) => { quotes++; return route.abort() })
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ body: route.request().url().includes('key=output') ? mp4 : png, contentType: route.request().url().includes('key=output') ? 'video/mp4' : 'image/png' }))
  await page.route('**/api/canvases**', (route) => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot
      return route.fulfill({ json: { version: ++canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.route('**/api/videos/generations', async (route) => {
    const { taskId, ...input } = route.request().postDataJSON()
    submissions.push(input)
    const now = new Date().toISOString()
    const task: GenerationTask = { id: taskId, kind: 'video', input, status: 'queued', createdAt: now, updatedAt: now }
    await route.fulfill({ status: 202, json: task })
    const asset = { key: 'output', url: '/api/assets/content?key=output', canvasId: canvasID, nodeId: targetID, kind: 'videos' as const, source: 'generated' as const, contentType: 'video/mp4', bytes: mp4.length, media: { width: 960, height: 540, durationSeconds: 2 } }
    task.status = 'succeeded'
    task.updatedAt = new Date(Date.now() + 100).toISOString()
    task.result = { model: input.model, url: asset.url, asset, durationSeconds: 2 }
    canvas.assets.push(asset)
    canvas.tasks = [task]; canvas.results = [task]
  })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '中转站视频', exact: true }).click({ position: { x: 60, y: 70 } })
  return { canvas, submissions, quotes: () => quotes }
}

test('APIMart 文生视频无需积分报价，结果回填可播放并在刷新后恢复', async ({ page }, info) => {
  const state = await setup(page)
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  const modes = page.getByRole('listbox', { name: '视频生成模式' })
  await expect(modes.getByRole('option', { name: /^文生视频/ })).toBeEnabled()
  await expect(modes.getByRole('option', { name: /^全能参考/ })).toBeEnabled()
  await modes.getByRole('option', { name: /^文生视频/ }).click()
  await expect(generate(page)).toBeDisabled()
  await editor(page).fill('A paper boat moves slowly on calm water.')
  await expect(generate(page)).toBeEnabled()
  await generate(page).click()
  const target = page.getByRole('article', { name: '中转站视频', exact: true })
  await expect(target.locator('video')).toHaveJSProperty('readyState', 4)
  expect(state.submissions).toHaveLength(1)
  expect(state.submissions[0]).toMatchObject({ provider: 'apimart', model: 'seedance-2.5', mode: 'text', ratio: '16:9', resolution: '720p', duration: 5, generateAudio: false })
  expect(state.submissions[0]).not.toHaveProperty('acceptedPoints')
  expect(state.submissions[0]).not.toHaveProperty('references')
  expect(state.submissions[0]).not.toHaveProperty('seed')
  expect(state.quotes()).toBe(0)
  await page.mouse.click(700, 100)
  await target.getByRole('button', { name: '播放视频', exact: true }).click()
  await expect.poll(() => target.locator('video').evaluate((video: HTMLVideoElement) => video.currentTime)).toBeGreaterThan(0)
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.reload()
  await expect(target.locator('video')).toHaveJSProperty('readyState', 4)
  await target.click({ position: { x: 60, y: 70 } })
  await expect(editor(page)).toHaveText('A paper boat moves slowly on calm water.')
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前文生视频/ })).toBeVisible()
  expect(state.canvas.snapshot.nodes[0]!.data.modelSelection!.parameters.seed).toBe(42)
  expect(state.submissions).toHaveLength(1)
  await page.screenshot({ path: info.outputPath('apimart-video-restored.png') })
})

test('APIMart 全能参考提交已保存图片，切换文生保留参考草稿，切换已接入型号保留兼容功能', async ({ page }) => {
  const state = await setup(page, 'image')
  await expect(page.getByRole('article', { name: '参考图片', exact: true })).not.toContainText('读取中')
  await expect(generate(page)).toBeDisabled()
  await editor(page).fill('Keep the colors of the reference image and animate the shapes slowly.')
  await expect(generate(page)).toBeEnabled()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await page.getByRole('option', { name: /^文生视频/ }).click()
  await expect(page.getByLabel('已连接但本次不使用的参考素材')).toBeVisible()
  expect(state.canvas.snapshot.nodes[0]!.data.referenceIds).toEqual([sourceID])
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await page.getByRole('option', { name: /^全能参考/ }).click()
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(1)
  expect(state.submissions[0]).toMatchObject({ provider: 'apimart', mode: 'reference', references: [{ nodeId: sourceID, assetKey: 'source', kind: 'image' }] })
  expect(state.quotes()).toBe(0)
  await expect(page.getByRole('article', { name: '中转站视频', exact: true }).locator('video')).toHaveJSProperty('readyState', 4)
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  await page.getByRole('option', { name: 'Kling 4.0（APIMart）', exact: true }).click()
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前全能参考/ })).toBeVisible()
  await expect(generate(page)).toBeEnabled()
})

test('APIMart 视频或音频参考不能绕过当前图片参考限制', async ({ page }) => {
  const state = await setup(page, 'audio')
  await editor(page).fill('Animate the scene.')
  await expect(page.getByText(/当前不支持音频参考/)).toBeVisible()
  await expect(generate(page)).toBeDisabled()
  expect(state.submissions).toHaveLength(0)
  expect(state.quotes()).toBe(0)
})

async function chooseModel(page: Page, name: string) {
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  await page.getByRole('option', { name: `${name}（APIMart）`, exact: true }).click()
}
async function chooseMode(page: Page, name: string) {
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await page.getByRole('option', { name: new RegExp(`^${name}`) }).click()
}

test('统一三项功能按型号置灰，空输入仍可先选功能，专用工作流型号不进入列表', async ({ page }) => {
  await setup(page)
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  for (const name of ['Kling 3.0 Motion Control', 'MiniMax H3 Regeneration', 'Wan 2.7 VideoEdit']) {
    await expect(page.getByRole('option', { name: `${name}（APIMart）`, exact: true })).toHaveCount(0)
  }
  await page.getByRole('option', { name: 'Vidu Q4 Preview（APIMart）', exact: true }).click()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  const menu = page.getByRole('listbox', { name: '视频生成模式' })
  await expect(menu.getByRole('option')).toHaveCount(3)
  await expect(menu.getByRole('option', { name: /^文生视频/ })).toBeDisabled()
  await expect(menu.getByRole('option', { name: /^全能参考/ })).toBeEnabled()
  await expect(menu.getByRole('option', { name: /^图生视频/ })).toBeEnabled()
  await menu.getByRole('option', { name: /^图生视频/ }).click()
  await expect(generate(page)).toBeDisabled()
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前图生视频/ })).toBeVisible()
})

test('切换模型保留兼容功能，不兼容时选择已支持的默认功能并保留素材草稿', async ({ page }) => {
  const state = await setup(page, 'image')
  await editor(page).fill('A geometric animation.')
  await chooseModel(page, 'Kling 3.0 Turbo')
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前文生视频/ })).toBeVisible()
  await expect(page.getByLabel('已连接但本次不使用的参考素材')).toBeVisible()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await expect(page.getByRole('option', { name: /^全能参考/ })).toBeDisabled()
  await page.getByRole('option', { name: /^图生视频/ }).click()
  await chooseModel(page, 'Vidu Q4 Preview')
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前图生视频/ })).toBeVisible()
  await chooseModel(page, 'Grok Imagine Video 1.5')
  await expect(page.getByRole('button', { name: /^选择视频生成类型，当前文生视频/ })).toBeVisible()
  await expect(editor(page)).toHaveText('A geometric animation.')
  expect(state.canvas.snapshot.nodes[0]!.data.referenceIds).toEqual([sourceID])
  expect(state.quotes()).toBe(0)
})

test('图生不会自动挑选已有参考图，明确选择后保存首帧角色并提交', async ({ page }, info) => {
  const state = await setup(page, 'image')
  await expect(page.getByRole('article', { name: '参考图片', exact: true })).not.toContainText('读取中')
  await editor(page).fill('The geometric shapes move gently.')
  await chooseMode(page, '图生视频')
  await expect(generate(page)).toBeDisabled()
  await expect(page.getByText('请明确选择一张起始图片', { exact: true })).toBeVisible()
  const firstFrame = page.getByRole('button', { name: '选为起始图片 参考图片', exact: true })
  await expect(firstFrame).toHaveAttribute('aria-pressed', 'false')
  await firstFrame.click()
  await expect(generate(page)).toBeEnabled()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  expect(state.canvas.snapshot.nodes[0]!.data.firstFrameId).toBe(sourceID)
  await page.reload()
  await page.getByRole('article', { name: '中转站视频', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(firstFrame).toHaveAttribute('aria-pressed', 'true')
  await expect(generate(page)).toBeEnabled()
  await page.screenshot({ path: info.outputPath('first-frame-restored.png') })
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(1)
  expect(state.submissions[0]).toMatchObject({ mode: 'image', provider: 'apimart', ratio: 'adaptive', parameters: { size: 'adaptive', resolution: '720p', duration: 5, generate_audio: false }, references: [{ nodeId: sourceID, assetKey: 'source', kind: 'image', role: 'first_frame' }] })
  expect(state.submissions[0]!.parameters).not.toHaveProperty('seed')
  expect(state.quotes()).toBe(0)
})

test('移除已指定起始图后禁用生成并清理首帧选择', async ({ page }) => {
  const state = await setup(page, 'image')
  await editor(page).fill('Animate the starting image.')
  await chooseMode(page, '图生视频')
  await page.getByRole('button', { name: '选为起始图片 参考图片', exact: true }).click()
  await expect(generate(page)).toBeEnabled()
  await page.getByRole('button', { name: '移除参考 参考图片', exact: true }).click()
  await expect(generate(page)).toBeDisabled()
  await expect.poll(() => state.canvas.snapshot.nodes[0]!.data.firstFrameId).toBeUndefined()
  expect(state.submissions).toHaveLength(0)
})

test('不同供应商字段随提交保存，未定义时长的型号不补造时长', async ({ page }) => {
  const state = await setup(page)
  await chooseModel(page, 'Gemini Omni 1.1 Flash')
  await chooseMode(page, '文生视频')
  await editor(page).fill('A calm cinematic landscape.')
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(1)
  expect(state.submissions[0]).toMatchObject({ model: 'gemini-omni-1.1-flash', mode: 'text', parameters: { aspect_ratio: '16:9', resolution: '720p', audio: true } })
  expect(state.submissions[0]).not.toHaveProperty('duration')
  expect(state.submissions[0]!.parameters).not.toHaveProperty('duration')
})

test('仅支持文生与图生的型号可使用首帧，不会被普通参考能力的限制误拦截', async ({ page }) => {
  const state = await setup(page, 'image')
  await editor(page).fill('The scene in the starting image moves gently.')
  for (const name of ['Kling 3.0 Turbo', 'Vidu Q3 Pro']) {
    await chooseModel(page, name)
    await chooseMode(page, '图生视频')
    await page.getByRole('button', { name: '选为起始图片 参考图片', exact: true }).click()
    await expect(generate(page)).toBeEnabled()
    await expect(page.getByRole('list', { name: '已选参考' })).not.toContainText('!')
  }
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(1)
  expect(state.submissions[0]).toMatchObject({ model: 'viduq3-pro', mode: 'image', parameters: { duration: 5, resolution: '720p' }, references: [{ role: 'first_frame', kind: 'image', nodeId: sourceID }] })
})
