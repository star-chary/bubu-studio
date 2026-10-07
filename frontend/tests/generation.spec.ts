import type { Page, Route } from '@playwright/test'
import { expect, test } from './setup'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'

const canvasId = '33333333-3333-4333-8333-333333333333'
const firstId = '11111111-1111-4111-8111-111111111111'
const secondId = '22222222-2222-4222-8222-222222222222'
const uploadId = '44444444-4444-4444-8444-444444444444'
const resultURL = 'https://generated.example.test/image.png'
const imageBuffer = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const panel = (page: Page) => page.getByRole('dialog', { name: '图片提示词', exact: true })

async function setup(page: Page) {
  const tasks: GenerationTask[] = []
  const canvas: SavedCanvas = { id: canvasId, title: '生成回归', version: 1, updatedAt: new Date().toISOString(), assets: [], tasks, results: [],
    snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: firstId, position: { x: 100, y: 150 }, data: { kind: 'image', name: '图片节点 1', prompt: '' } },
      { id: secondId, position: { x: 820, y: 150 }, data: { kind: 'image', name: '图片节点 2', prompt: '' } },
      { id: uploadId, position: { x: 820, y: 520 }, data: { kind: 'image', name: '上传图片', origin: 'upload' } },
    ] } }
  const bodies: Record<string, unknown>[] = []
  function succeededTask(body: Record<string, unknown>): GenerationTask {
    const { taskId, acceptedPoints, priceVersion, ...input } = body
    const now = new Date().toISOString()
    const task: GenerationTask = { id: String(taskId), kind: 'image', input: input as unknown as GenerationTask['input'],
      status: 'succeeded', result: { url: resultURL, model: String(body.model), size: '960x540' },
      creditPoints: Number(acceptedPoints), creditPriceVersion: String(priceVersion), creditStatus: 'settled', createdAt: now, updatedAt: now }
    tasks.unshift(task)
    return task
  }
  let submit: (route: Route, body: Record<string, unknown>, call: number) => Promise<void> = async (route, body) => {
    await route.fulfill({ status: 202, json: succeededTask(body) })
  }
  await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
  await page.route(resultURL, route => route.fulfill({ contentType: 'image/png', body: imageBuffer }))
  await page.route('**/api/canvases**', route => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks, hasMore: false } })
    if (route.request().method() === 'PUT') {
      canvas.snapshot = route.request().postDataJSON().snapshot; canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: { ...canvas, results: tasks.filter(task => task.status === 'succeeded') } })
  })
  await page.route('**/api/tasks/*', route => {
    const task = tasks.find(item => route.request().url().endsWith(item.id))
    return route.fulfill({ status: task ? 200 : 404, json: task ?? { error: { message: '任务不存在' } } })
  })
  await page.route('**/api/images/generations', async route => {
    const body = route.request().postDataJSON() as Record<string, unknown>
    bodies.push(body)
    await submit(route, body, bodies.length)
  })
  await page.goto(`/?canvas=${canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  return { bodies, succeededTask, setSubmit(fn: typeof submit) { submit = fn } }
}

test('图片任务固定已报价的模型和提示词，切换节点后结果回填原节点', async ({ page }) => {
  const db = await setup(page)
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  db.setSubmit(async (route, body) => { await pending; await route.fulfill({ status: 202, json: db.succeededTask(body) }) })
  const first = page.getByRole('article', { name: '图片节点 1', exact: true })
  const second = page.getByRole('article', { name: '图片节点 2', exact: true })
  await first.click({ position: { x: 70, y: 80 } })
  await panel(page).getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: 'Doubao-Seedream-5.0-pro', exact: true }).click()
  await panel(page).getByRole('textbox').fill('黑洞中的复古列车')
  await expect(panel(page).locator('.credit-cost-trigger')).toHaveText('16')
  await panel(page).getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(panel(page).getByRole('button', { name: '生成中…' })).toBeDisabled()
  await panel(page).getByRole('textbox').fill('下一次生成的草稿')
  await second.click({ position: { x: 70, y: 80 } })
  release()
  await expect(first.locator('img')).toHaveAttribute('src', resultURL)
  await expect(second.getByLabel('空图片节点')).toBeVisible()
  expect(db.bodies).toHaveLength(1)
  expect(db.bodies[0]).toMatchObject({ prompt: '黑洞中的复古列车', model: 'doubao-seedream-5-0-pro-260628',
    acceptedPoints: 16, priceVersion: 'test-v1', canvasId, nodeId: firstId })
  await first.click({ position: { x: 70, y: 80 } })
  await expect(panel(page).getByRole('textbox')).toHaveText('下一次生成的草稿')
})

test('任务提交被拒绝时保留上次图片和当前草稿，可手动重试', async ({ page }) => {
  const db = await setup(page)
  db.setSubmit(async (route, body, call) => {
    if (call === 2) return route.fulfill({ status: 429, json: { error: { code: 'MODEL_LIMITED', message: '模型额度受限，请检查控制台。' } } })
    return route.fulfill({ status: 202, json: db.succeededTask(body) })
  })
  const target = page.getByRole('article', { name: '图片节点 1', exact: true })
  await target.click({ position: { x: 70, y: 80 } })
  await panel(page).getByRole('textbox').fill('第一张图片')
  await panel(page).getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(target.locator('img')).toHaveAttribute('src', resultURL)
  await panel(page).getByRole('textbox').fill('第二张图片')
  await panel(page).getByRole('button', { name: '生成图片', exact: true }).click()
  await expect(panel(page).getByRole('alert')).toContainText('模型额度受限')
  await expect(target.locator('img')).toHaveAttribute('src', resultURL)
  await expect(panel(page).getByRole('textbox')).toHaveText('第二张图片')
  expect(db.bodies).toHaveLength(2)
  await panel(page).getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.bodies.length).toBe(3)
  expect(new Set(db.bodies.map(body => body.taskId)).size).toBe(3)
})

test('图片空提示词和超长文本不提交，上传节点没有生成按钮', async ({ page }) => {
  const db = await setup(page)
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 70, y: 80 } })
  const button = panel(page).getByRole('button', { name: '生成图片', exact: true })
  await expect(button).toBeDisabled()
  await panel(page).getByRole('textbox').fill('   ')
  await expect(button).toBeDisabled()
  await panel(page).getByRole('textbox').fill('图'.repeat(2001))
  await expect(button).toBeDisabled()
  await panel(page).getByRole('textbox').fill('测试提示词')
  await expect(button).toBeEnabled()
  await page.getByRole('article', { name: '上传图片', exact: true }).click({ position: { x: 70, y: 80 } })
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toHaveCount(0)
  expect(db.bodies).toHaveLength(0)
})

test('报价接口断连保留草稿，重新获取报价后才能提交', async ({ page }) => {
  const db = await setup(page)
  await page.route('**/api/credits/quote', route => route.abort())
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 70, y: 80 } })
  await panel(page).getByRole('textbox').fill('保留这段描述')
  await expect(panel(page).getByRole('status').filter({ hasText: '报价获取失败' })).toBeVisible()
  await panel(page).locator('.credit-cost-trigger').click()
  await expect(panel(page).locator('.credit-cost-popover')).toContainText('无法获取报价')
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  await page.route('**/api/credits/quote', route => route.fulfill({ json: { points: 6, priceVersion: 'test-v1' } }))
  await panel(page).getByRole('button', { name: '重新获取报价' }).click()
  await expect(panel(page).getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  await expect(panel(page).getByRole('textbox')).toHaveText('保留这段描述')
  expect(db.bodies).toHaveLength(0)
})
