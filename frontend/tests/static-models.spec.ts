import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import { STATIC_MODELS, catalogModelFor, catalogModelsFor, modelOptionIdFor } from '../src/models/staticModelCatalog'
import { defaultParametersFor, normalizeParametersFor, parameterValuesFor, validParameterValue, type ModelSelection } from '../src/models/modelCatalog'
import type { SavedCanvas } from '../src/api/persistence'

const canvasID = '33333333-3333-4333-8333-333333333333'
const nodeID = '11111111-1111-4111-8111-111111111111'
const selector = (page: Page) => page.getByRole('button', { name: /^选择(模型|视频模型)/ })
const settings = (page: Page, kind: 'image' | 'video') => page.getByRole('button', { name: kind === 'image' ? /^图片参数，/ : /^视频参数，/ })

async function setupCanvas(page: Page, kind: 'image' | 'video', selection: ModelSelection = { provider: 'apimart', modelId: '', parameters: {} }) {
  const canvas: SavedCanvas = { id: canvasID, title: '静态模型测试', version: 1, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString(), snapshot: {
    viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: nodeID, position: { x: 100, y: 150 }, data: { kind, name: '测试节点', prompt: '', modelSelection: structuredClone(selection) } }],
  } }
  let forbidden = 0
  page.on('request', (request) => {
    if (/\/api\/(model-catalog|credits\/quote|images\/generations|videos\/generations)(?:\?|$)/.test(request.url())) forbidden++
  })
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', (route) => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot
      return route.fulfill({ json: { version: ++canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '测试节点', exact: true }).click({ position: { x: 70, y: 80 } })
  return { selection: () => canvas.snapshot.nodes[0]!.data.modelSelection!, canvas, forbidden: () => forbidden }
}
async function choose(page: Page, optionId: string) {
  await selector(page).click()
  const menu = page.getByRole('listbox', { name: /模型$/ })
  await menu.locator(`[data-provider="apimart"][data-model-option="${optionId}"]`).click()
  await expect(menu).toHaveCount(0)
}
async function parameters(page: Page, kind: 'image' | 'video') {
  await settings(page, kind).click()
  return page.getByRole('dialog', { name: kind === 'image' ? '图片参数' : '视频参数', exact: true })
}

test('固定清单只包含采纳的主力与候选，基础参数都有文档来源和合法初值', () => {
  expect(catalogModelsFor('image')).toHaveLength(16)
  expect(catalogModelsFor('video')).toHaveLength(22)
  expect(new Set(STATIC_MODELS.map((model) => model.optionId)).size).toBe(STATIC_MODELS.length)
  expect(STATIC_MODELS.filter((model) => model.modelId === 'midjourney').map((model) => model.optionId)).toEqual([
    'midjourney-8.2', 'midjourney-8.1', 'midjourney-7', 'midjourney-niji7',
  ])
  for (const obsolete of ['dall-e-3', 'gpt-image-1', 'seedance-1-5-pro', 'MiniMax-H3-Context-IR']) {
    expect(STATIC_MODELS.some((model) => model.modelId === obsolete)).toBe(false)
  }
  expect(catalogModelFor('grok-imagine-video-1.5', 'video')?.group).toBe('primary')
  for (const model of STATIC_MODELS) {
    expect(model.document).toMatch(/^https:\/\/docs\.apimart\.ai\//)
    expect(model.generationEnabled).toBe(model.category === 'image' || (model.category === 'video' && !!model.videoModes?.length))
    const defaults = defaultParametersFor(model)
    for (const field of model.parameters!.parameters) {
      expect(['ratio', 'resolution', 'duration', 'audio', 'count']).toContain(field.role)
      expect(['enum', 'integer', 'boolean', 'fixed']).toContain(field.kind)
      if (defaults[field.key] !== undefined) expect(validParameterValue(field, defaults[field.key], defaults), `${model.optionId}.${field.key}`).toBe(true)
    }
  }
})

test('参数按供应商原字段保存，时长特殊值、固定数量和联动不伪造范围', () => {
  const seedance = catalogModelFor('seedance-2.5', 'video')!
  const duration = seedance.parameters!.parameters.find((field) => field.role === 'duration')!
  expect(validParameterValue(duration, -1, {})).toBe(true)
  for (const value of [0, 3, 31]) expect(validParameterValue(duration, value, {})).toBe(false)
  expect(normalizeParametersFor(seedance, { aspect_ratio: '9:16', duration: 8, generate_audio: false }).value).toMatchObject({ size: '9:16', duration: 8, generate_audio: false })
  expect(catalogModelFor('gemini-omni-1.1-flash', 'video')!.parameters!.parameters.some((field) => field.role === 'duration')).toBe(false)
  const kling = catalogModelFor('kling-v3', 'video')!
  expect(kling.parameters!.parameters.find((field) => field.role === 'resolution')!.key).toBe('mode')
  const single = catalogModelFor('seedream-5-0-pro', 'image')!
  expect(single.parameters!.parameters.find((field) => field.role === 'count')).toMatchObject({ kind: 'fixed', fixed: 1 })
  const raw = { aspect_ratio: '4:3', seed: 42, quality: 'high' }
  expect(parameterValuesFor(seedance, raw).size).toBe('4:3')
  expect(raw).toEqual({ aspect_ratio: '4:3', seed: 42, quality: 'high' })
})

test('四个 Midjourney 选项共用上游身份，Niji 与版本独立且不把质量当成清晰度', () => {
  for (const [id, version, niji] of [['midjourney-8.2', '8.2', false], ['midjourney-8.1', '8.1', false], ['midjourney-7', '7', false], ['midjourney-niji7', '7', true]] as const) {
    const model = catalogModelFor(id, 'image')!
    const value = normalizeParametersFor(model, { version: '6.1', niji: !niji }).value
    expect(value).toMatchObject({ version, niji })
    expect(modelOptionIdFor({ provider: 'apimart', modelId: model.modelId, parameters: value }, 'image')).toBe(id)
    expect(model.modelId).toBe('midjourney')
    expect(model.parameters!.parameters.some((field) => field.key === 'quality')).toBe(false)
  }
  expect(catalogModelFor('midjourney', 'image', { version: '6.1' })).toBeUndefined()
})

test('初次打开与过期缓存均不请求目录，所有固定型号都能立即展开基础面板', async ({ page }, info) => {
  test.setTimeout(120_000)
  await page.addInitScript(() => {
    const request = indexedDB.open('frame-space-model-catalog', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('catalogs')
    request.onsuccess = () => {
      const db = request.result
      const tx = db.transaction('catalogs', 'readwrite')
      tx.objectStore('catalogs').put({ storedAt: 0, formatVersion: 1, payload: { status: 'loading', models: [] } }, '99999999-9999-4999-8999-999999999999:apimart')
      tx.oncomplete = () => db.close()
    }
  })
  for (const kind of ['image', 'video'] as const) {
    const db = await setupCanvas(page, kind)
    for (const model of catalogModelsFor(kind)) {
      await choose(page, model.optionId)
      const dialog = await parameters(page, kind)
      await expect(dialog.locator('fieldset')).toHaveCount(model.parameters!.parameters.length)
      await expect(dialog.locator('details')).toHaveCount(0)
      await expect(dialog).not.toContainText('可选范围未提供')
      await expect(dialog).not.toContainText('更多设置')
      if (['seedance-2.5', 'midjourney-8.2', 'gpt-image-2.5-flare', 'kling-v3', 'grok-imagine-video-1.5'].includes(model.optionId)) {
        await dialog.screenshot({ path: info.outputPath(`${model.optionId}.png`) })
      }
      await page.keyboard.press('Escape')
    }
    expect(db.forbidden()).toBe(0)
  }
})

test('Midjourney 版本切换和高清布尔值保存恢复，不串到 Niji，模型只选中一项', async ({ page }, info) => {
  const db = await setupCanvas(page, 'image')
  await choose(page, 'midjourney-8.2')
  let dialog = await parameters(page, 'image')
  await dialog.getByRole('radio', { name: '高清', exact: true }).check()
  await expect.poll(() => db.selection().parameters.hd).toBe(true)
  await page.keyboard.press('Escape')
  await choose(page, 'midjourney-niji7')
  await expect.poll(() => db.selection().parameters).toMatchObject({ version: '7', niji: true })
  expect(db.selection().modelId).toBe('midjourney')
  expect(db.selection().parameters.hd).toBeUndefined()
  await page.reload()
  await page.getByRole('article', { name: '测试节点', exact: true }).click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText('Niji 7')
  await selector(page).click()
  await expect(page.locator('[role="option"][aria-selected="true"]')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await choose(page, 'midjourney-7')
  await expect.poll(() => db.selection().parameters.niji).toBe(false)
  await choose(page, 'midjourney-8.1')
  dialog = await parameters(page, 'image')
  await expect(dialog.getByRole('radio', { name: '标准', exact: true })).toBeChecked()
  await dialog.screenshot({ path: info.outputPath('midjourney-8.1.png') })
  expect(db.forbidden()).toBe(0)
})

test('历史基础字段别名能回显，编辑只替换该项，未展示高级字段仍保存', async ({ page }, info) => {
  const db = await setupCanvas(page, 'video', { provider: 'apimart', modelId: 'seedance-2.5', parameters: {
    aspect_ratio: '4:3', resolution: '720p', duration: 8, seed: 42, output_format: 'mov', custom: { untouched: true },
  } })
  const dialog = await parameters(page, 'video')
  await expect(dialog.getByRole('radio', { name: '4:3', exact: true })).toBeChecked()
  await expect(dialog.getByRole('slider')).toHaveAttribute('min', '4')
  await expect(dialog.getByRole('slider')).toHaveAttribute('max', '30')
  await dialog.getByRole('radio', { name: '9:16', exact: true }).check()
  await expect.poll(() => db.selection().parameters.size).toBe('9:16')
  expect(db.selection().parameters.aspect_ratio).toBeUndefined()
  expect(db.selection().parameters).toMatchObject({ seed: 42, output_format: 'mov', custom: { untouched: true } })
  await dialog.getByRole('radio', { name: '自动时长', exact: true }).check()
  await expect.poll(() => db.selection().parameters.duration).toBe(-1)
  await page.setViewportSize({ width: 820, height: 640 })
  await expect.poll(async () => {
    const box = await dialog.boundingBox()
    return !!box && box.x >= 15 && box.y >= 15 && box.x + box.width <= 805 && box.y + box.height <= 625
  }).toBe(true)
  await dialog.screenshot({ path: info.outputPath('basic-parameters-small-viewport.png') })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.reload()
  await page.getByRole('article', { name: '测试节点', exact: true }).click({ position: { x: 70, y: 80 } })
  const restored = await parameters(page, 'video')
  await expect(restored.getByRole('radio', { name: '自动时长', exact: true })).toBeChecked()
  expect(db.selection().parameters.custom).toEqual({ untouched: true })
  expect(db.forbidden()).toBe(0)
})

test('生成数量保持整数，固定数量只读，原生 mode 和音频字段正确保存', async ({ page }) => {
  const image = await setupCanvas(page, 'image')
  await choose(page, 'gpt-image-2.5-flare')
  let dialog = await parameters(page, 'image')
  await dialog.getByRole('group', { name: '生成数量', exact: true }).getByRole('radio', { name: '4张' }).check()
  await expect.poll(() => image.selection().parameters.n).toBe(4)
  await page.keyboard.press('Escape')
  await choose(page, 'seedream-5-0-pro')
  dialog = await parameters(page, 'image')
  await expect(dialog.getByRole('group', { name: '生成数量', exact: true })).toHaveText(/生成数量\s*1张/)
  await expect(dialog.getByRole('group', { name: '生成数量', exact: true }).getByRole('radio')).toHaveCount(0)
  const video = await setupCanvas(page, 'video')
  await choose(page, 'kling-v3')
  dialog = await parameters(page, 'video')
  await dialog.getByRole('radio', { name: '1080P', exact: true }).check()
  await dialog.getByRole('checkbox', { name: '生成音频', exact: true }).check()
  await expect.poll(() => video.selection().parameters.mode).toBe('pro')
  await expect.poll(() => video.selection().parameters.audio).toBe(true)
  await page.keyboard.press('Escape')
  await choose(page, 'kling-v4')
  dialog = await parameters(page, 'video')
  await expect(dialog.getByRole('checkbox', { name: '生成音频', exact: true })).toBeChecked()
  await expect(dialog.getByRole('checkbox', { name: '生成音频', exact: true })).toBeDisabled()
})

test('清晰度改变会调整不兼容时长，模型不支持的时长控件不展示', async ({ page }) => {
  const db = await setupCanvas(page, 'video')
  await choose(page, 'veo3.1-quality-official')
  let dialog = await parameters(page, 'video')
  await expect(dialog.getByRole('group', { name: '生成数量', exact: true })).toContainText('1个')
  await expect(dialog.getByRole('group', { name: '生成数量', exact: true }).getByRole('radio')).toHaveCount(0)
  await dialog.getByRole('group', { name: '视频时长', exact: true }).getByRole('radio', { name: '4s' }).check()
  await expect.poll(() => db.selection().parameters.duration).toBe(4)
  await dialog.getByRole('radio', { name: '4K', exact: true }).check()
  await expect.poll(() => db.selection().parameters.duration).toBe(8)
  await expect(dialog.getByRole('group', { name: '视频时长', exact: true }).getByRole('radio')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await choose(page, 'gemini-omni-1.1-flash')
  dialog = await parameters(page, 'video')
  await expect(dialog.getByRole('group', { name: '视频时长', exact: true })).toHaveCount(0)
  await expect.poll(() => db.selection().modelId).toBe('gemini-omni-1.1-flash')
  expect(db.selection().parameters.duration).toBeUndefined()
  expect(db.forbidden()).toBe(0)
})

test('已过滤历史型号保留原始选择和复杂参数，列表无法重新选入且不会误走方舟', async ({ page }) => {
  const original: ModelSelection = { provider: 'apimart', modelId: 'gpt-image-1', parameters: { quality: 'high', size: '1024x1024', old: { keep: [1, 2] } } }
  const db = await setupCanvas(page, 'image', original)
  await expect(selector(page)).toContainText('gpt-image-1')
  await selector(page).click()
  await expect(page.locator('[data-model-option="gpt-image-1"]')).toHaveCount(0)
  await expect(page.getByRole('listbox', { name: '图片模型' }).getByRole('status')).toContainText('历史模型')
  await page.keyboard.press('Escape')
  const dialog = await parameters(page, 'image')
  await expect(dialog).toContainText('原选择与参数继续保留')
  await page.keyboard.press('Escape')
  await page.getByRole('textbox', { name: '图片提示词', exact: true }).fill('编辑提示词以触发保存')
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  expect(db.selection()).toEqual(original)
  expect(db.forbidden()).toBe(0)
})
