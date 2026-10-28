import { readFileSync } from 'node:fs'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { GenerationTask, SavedCanvas, TaskInput } from '../src/api/persistence'
import { apimartImageInput } from '../src/models/apimartImage'
import { catalogModelsFor } from '../src/models/staticModelCatalog'
import { defaultParametersFor } from '../src/models/modelCatalog'

const canvasID = '11111111-1111-4111-8111-111111111111'
const targetID = '22222222-2222-4222-8222-222222222222'
const sourceID = '33333333-3333-4333-8333-333333333333'
const editor = (page: Page) => page.getByRole('textbox', { name: '图片提示词', exact: true })
const generate = (page: Page) => page.getByRole('button', { name: '生成图片', exact: true })

test('每个图片型号的默认参数都能构建单图请求', () => {
  for (const model of catalogModelsFor('image')) {
    const selection = { provider: 'apimart' as const, modelId: model.modelId, parameters: defaultParametersFor(model) }
    const input = apimartImageInput(selection, { canvasId: canvasID, nodeId: targetID, prompt: 'A paper boat' }, [])
    expect(input, model.optionId).not.toBeNull()
    expect(input!.model).toBe(model.modelId)
    if ('n' in (input!.parameters ?? {})) expect(input!.parameters!.n).toBe(1)
  }
  const oldMidjourney = { provider: 'apimart' as const, modelId: 'midjourney', parameters: { version: '7', niji: true, size: '1:1', hd: true } }
  const request = apimartImageInput(oldMidjourney, { canvasId: canvasID, nodeId: targetID, prompt: 'A paper boat' }, [])
  expect(request).toBeNull()
})

async function setup(page: Page, model = 'seedream-5-0-lite', count = 1, reference = false, available = 200) {
  const png = readFileSync(new URL('./fixtures/sample-image.png', import.meta.url))
  const canvas: SavedCanvas = { id: canvasID, version: 1, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [], snapshot: {
    viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: targetID, position: { x: 80, y: 130 }, data: {
      kind: 'image', name: '中转站图片', prompt: '', referenceIds: reference ? [sourceID] : [],
      modelSelection: { provider: 'apimart', modelId: model, parameters: model === 'seedream-5-0-lite' ? { size: '1:1', resolution: '2K', n: count, seed: 42 } : {} },
    } }],
  } }
  if (reference) {
    canvas.snapshot.nodes.push({ id: sourceID, position: { x: 800, y: 130 }, data: { kind: 'image', name: '参考图片', origin: 'upload' } })
    canvas.assets.push({ key: 'source', url: '/api/assets/content?key=source', canvasId: canvasID, nodeId: sourceID, kind: 'images', source: 'uploads', contentType: 'image/png', bytes: png.length, media: { width: 960, height: 540 } })
  }
  const submissions: TaskInput[] = []
  let quotes = 0
  await page.route('**/api/credits', (route) => route.fulfill({ json: { available, reserved: 0 } }))
  await page.route('**/api/credits/quote', (route) => { quotes++; return route.fulfill({ json: { points: 6, priceVersion: 'apimart-test-v1' } }) })
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ body: png, contentType: 'image/png' }))
  await page.route('**/api/canvases**', (route) => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot
      return route.fulfill({ json: { version: ++canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.route('**/api/images/generations', async (route) => {
    const { taskId, ...input } = route.request().postDataJSON()
    submissions.push(input)
    const now = new Date().toISOString()
    const task: GenerationTask = { id: taskId, kind: 'image', input, status: 'queued', createdAt: now, updatedAt: now, creditStatus: 'legacy' }
    await route.fulfill({ status: 202, json: task })
    const key = `output-${taskId}`
    const asset = { key, url: `/api/assets/content?key=${key}`, canvasId: canvasID, nodeId: input.nodeId, kind: 'images' as const, source: 'generated' as const, contentType: 'image/png', bytes: png.length }
    task.status = 'succeeded'; task.updatedAt = new Date(Date.now() + 100).toISOString()
    task.result = { model: input.model, url: asset.url, asset }
    canvas.assets.push(asset); canvas.tasks.push(task); canvas.results.push(task)
  })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '中转站图片', exact: true }).click({ position: { x: 60, y: 70 } })
  return { canvas, submissions, quotes: () => quotes }
}

test('多张中转站图片显示总报价，每张携带单价提交并在刷新后恢复', async ({ page }) => {
  const state = await setup(page, 'seedream-5-0-lite', 2)
  await editor(page).fill('A paper boat on a calm lake.')
  await expect(generate(page)).toBeEnabled()
  await expect(page.locator('.credit-cost-trigger')).toHaveText('12')
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(2)
  expect(state.submissions.every((item) => item.provider === 'apimart' && item.model === 'seedream-5-0-lite')).toBe(true)
  expect(state.submissions.map((item) => item.parameters)).toEqual([{ size: '1:1', resolution: '2K', n: 1 }, { size: '1:1', resolution: '2K', n: 1 }])
  expect(state.submissions.every((item) => item.acceptedPoints === 6 && item.priceVersion === 'apimart-test-v1')).toBe(true)
  expect(state.submissions[0]!.nodeId).not.toBe(state.submissions[1]!.nodeId)
  expect(state.quotes()).toBeGreaterThan(0)
  await expect(page.getByRole('article', { name: '中转站图片', exact: true }).locator('img')).toBeVisible()
  await expect(page.getByRole('article', { name: '中转站图片 · 2', exact: true }).locator('img')).toBeVisible()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.reload()
  await expect(page.getByRole('article', { name: '中转站图片', exact: true }).locator('img')).toBeVisible()
  await expect(page.getByRole('article', { name: '中转站图片 · 2', exact: true }).locator('img')).toBeVisible()
  expect(state.submissions).toHaveLength(2)
  expect(state.canvas.snapshot.nodes[0]!.data.modelSelection!.parameters.seed).toBe(42)
})

test('图生图提交已保存素材 Key，不支持参考的型号阻止提交', async ({ page }) => {
  const state = await setup(page, 'seedream-5-0-lite', 1, true)
  await editor(page).fill('Turn the reference image into a sketch.')
  await expect(generate(page)).toBeEnabled()
  await generate(page).click()
  await expect.poll(() => state.submissions.length).toBe(1)
  expect(state.submissions[0]!.referenceKeys).toEqual(['source'])
  expect(state.submissions[0]).not.toHaveProperty('image_urls')
  await setup(page, 'z-image-turbo', 1, true)
  await editor(page).fill('Make a sketch.')
  await expect(page.getByText(/不支持参考图片/)).toBeVisible()
  await expect(generate(page)).toBeDisabled()
})

test('中转站图片余额不足时仍展示固定报价，但不发送生成请求', async ({ page }) => {
  const state = await setup(page, 'seedream-5-0-lite', 2, false, 11)
  await editor(page).fill('A paper boat.')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('12')
  await expect(page.getByText('积分不足', { exact: true })).toBeVisible()
  await expect(generate(page)).toBeDisabled()
  expect(state.submissions).toHaveLength(0)
})

test('历史草稿参数无效时提示检查参数，不能出现可点击但无反应的生成按钮', async ({ page }) => {
  await setup(page, 'seedream-5-0-lite', 99)
  await editor(page).fill('A paper boat.')
  await expect(generate(page)).toBeDisabled()
  await expect(page.getByText('模型参数不完整或无效，请重新选择参数后生成')).toBeVisible()
})
