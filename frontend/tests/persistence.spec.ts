import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { CanvasSnapshot, GenerationTask, SavedCanvas } from '../src/api/persistence'

const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const resultURL = 'https://persistence.example.test/generated.png'
const model = 'doubao-seedream-5-0-260128'
const sourceID = '11111111-1111-4111-8111-111111111111'
const targetID = '22222222-2222-4222-8222-222222222222'
const canvasID = '33333333-3333-4333-8333-333333333333'

async function databaseMock(page: Page, initial?: SavedCanvas) {
  let canvas: SavedCanvas = initial ?? { id: canvasID, title: '测试画布', version: 0, snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } }, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString() }
  let tasks: GenerationTask[] = initial?.tasks ?? []
  let calls = 0
  let failSave = false
  let conflict = false
  let failLoad = false
  let saveDelay: Promise<void> | undefined
  const savedBodies: CanvasSnapshot[] = []
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route(resultURL, (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/canvases**', async (route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    if (url.pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks, hasMore: false } })
    if (method === 'POST') {
      const { id } = route.request().postDataJSON()
      canvas = { id, version: 0, snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } }, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString() }
      return route.fulfill({ status: 201, json: canvas })
    }
    if (method === 'PUT') {
      if (saveDelay) await saveDelay
      if (failSave) return route.fulfill({ status: 503, json: { error: { code: 'DATABASE_UNAVAILABLE', message: '数据库暂时不可用' } } })
      const body = route.request().postDataJSON()
      if (conflict || body.version !== canvas?.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '画布已在其他页面更新，请刷新后重试。' } } })
      canvas!.version++
      canvas!.snapshot = body.snapshot
      savedBodies.push(body.snapshot)
      return route.fulfill({ json: { version: canvas!.version } })
    }
    if (failLoad) return route.fulfill({ status: 503, json: { error: { message: '数据库暂时不可用' } } })
    if (!canvas) return route.fulfill({ status: 404, json: { error: { message: '画布不存在' } } })
    return route.fulfill({ json: { ...canvas, tasks, results: tasks.filter((t) => t.status === 'succeeded') } })
  })
  await page.route('**/api/tasks/*', (route) => {
    const task = tasks.find((t) => route.request().url().endsWith(t.id))
    return route.fulfill({ status: task ? 200 : 404, json: task ?? { error: { message: 'task missing' } } })
  })
  await page.route('**/api/images/generations', async (route) => {
    calls++
    const { taskId, ...input } = route.request().postDataJSON()
    expect(canvas?.snapshot.nodes.some((n) => n.id === input.nodeId)).toBe(true)
    const task: GenerationTask = { id: taskId, input, status: 'running', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }
    tasks = [task, ...tasks]
    await route.fulfill({ status: 202, json: task })
  })
  return {
    get canvas() { return canvas! }, get tasks() { return tasks }, get calls() { return calls }, savedBodies,
    failSave(value: boolean) { failSave = value }, conflict() { conflict = true }, failLoad(value: boolean) { failLoad = value },
    delaySave(value?: Promise<void>) { saveDelay = value },
    progress(status: GenerationTask['status']) {
      tasks[0] = { ...tasks[0]!, status, updatedAt: new Date().toISOString() }
    },
    complete(status: 'succeeded' | 'failed' | 'interrupted' = 'succeeded') {
      tasks[0] = { ...tasks[0]!, status, updatedAt: new Date().toISOString(), finishedAt: new Date().toISOString(),
        result: status === 'succeeded' ? { url: resultURL, model } : undefined,
        error: status === 'succeeded' ? undefined : { code: 'TEST_FAILURE', message: '任务已中断，未自动重新生成。' } }
    },
  }
}
async function addImage(page: Page) {
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.mouse.click(220, 150, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const node = page.getByRole('article', { name: '图片节点 1', exact: true })
  await node.click({ position: { x: 60, y: 70 } })
  return node
}
async function saved(page: Page) { await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存') }

test('排队任务刷新后保留，准备素材和保存阶段持续追踪且不重复提交', async ({ page }) => {
  const db = await databaseMock(page)
  await page.goto(`/?canvas=${canvasID}`)
  const node = await addImage(page)
  await page.getByRole('textbox').fill('排队后自动执行的任务')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.calls).toBe(1)
  db.progress('queued')
  await saved(page)
  await page.reload()
  await expect(node.getByRole('status')).toHaveText('等待执行…')
  await node.click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('button', { name: '等待执行…', exact: true })).toBeDisabled()
  db.progress('preparing')
  await expect(node.getByRole('status')).toHaveText('准备素材…')
  await expect(page.getByRole('button', { name: '准备素材…', exact: true })).toBeDisabled()
  db.progress('running')
  await expect(node.getByRole('status')).toHaveText('图片生成中…')
  db.progress('saving')
  await expect(node.getByRole('status')).toHaveText('保存中…')
  db.complete()
  await expect(node.locator('img')).toHaveAttribute('src', resultURL)
  expect(db.calls).toBe(1)
})

test('保存并刷新恢复草稿、模型、节点位置与视口，地址保留画布 ID', async ({ page }) => {
  const db = await databaseMock(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  const node = await addImage(page)
  await page.getByRole('textbox').fill('刷新后仍保留的创作草稿')
  await page.getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: 'Doubao-Seedream-5.0-pro', exact: true }).click()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved(page)
  const before = JSON.parse(JSON.stringify(db.canvas.snapshot))
  await page.reload()
  await saved(page)
  await expect(page).toHaveURL(new RegExp(`canvas=${db.canvas.id}`))
  await expect(page.getByLabel('当前缩放比例')).toHaveText('120%')
  await node.click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox')).toHaveText('刷新后仍保留的创作草稿')
  await expect(page.getByRole('button', { name: /^选择模型/ })).toContainText('pro')
  expect(db.canvas.snapshot).toEqual(before)
  expect(db.savedBodies.every((snapshot) => !JSON.stringify(snapshot).includes('blob:'))).toBe(true)
})

test('刷新后继续查询运行任务，结果回填原节点且保留提交后的草稿，任务记录显示快照', async ({ page }, testInfo) => {
  const db = await databaseMock(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  const node = await addImage(page)
  await page.getByRole('textbox').fill('提交时的提示词')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.calls).toBe(1)
  await expect(page.getByRole('button', { name: '生成中…' })).toBeDisabled()
  await page.getByRole('textbox').fill('提交后继续编辑的草稿')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved(page)
  await page.reload()
  await saved(page)
  await expect(node).toContainText('图片生成中…')
  await node.click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox')).toHaveText('提交后继续编辑的草稿')
  db.complete()
  await expect(node.locator('img')).toHaveAttribute('src', resultURL)
  await expect(node).toContainText('960 × 540')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '任务记录', exact: true }).click()
  const history = page.getByLabel('生成任务记录')
  await expect(history).toContainText('已完成')
  await expect(history).toContainText('提交时的提示词')
  await expect(history).toContainText(db.tasks[0]!.id)
  expect(db.calls).toBe(1)
  await page.screenshot({ path: testInfo.outputPath('persistence-task-recovered.png') })
  await saved(page)
  await page.reload()
  await expect(node.locator('img')).toHaveAttribute('src', resultURL)
  expect(db.calls).toBe(1)
})

test('恢复已保存素材及引用连线，未上传文件显示明确占位', async ({ page }) => {
  const key = `frame-space/dev/canvases/${canvasID}/uploads/images/${sourceID}/44444444-4444-4444-8444-444444444444.png`
  const asset = { canvasId: canvasID, nodeId: sourceID, key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, kind: 'images' as const, source: 'uploads' as const, bytes: 123, contentType: 'image/png' }
  const initial: SavedCanvas = { id: canvasID, version: 1, updatedAt: new Date().toISOString(), tasks: [], results: [], assets: [asset], snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
    { id: sourceID, position: { x: 80, y: 150 }, data: { kind: 'image', name: '已上传图片', origin: 'upload' } },
    { id: targetID, position: { x: 770, y: 150 }, data: { kind: 'image', name: '图片节点 1', prompt: '参考图制作', referenceIds: [sourceID] } },
    { id: '55555555-5555-4555-8555-555555555555', position: { x: 80, y: 520 }, data: { kind: 'image', name: '未上传文件', origin: 'upload' } },
  ] } }
  await databaseMock(page, initial)
  await page.goto(`/?canvas=${canvasID}`)
  await saved(page)
  await expect(page.getByRole('article', { name: '已上传图片' })).toContainText('960 × 540')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  await expect(page.getByRole('article', { name: '未上传文件' })).toContainText('重新上传')
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox')).toHaveText('参考图制作')
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
})

test('保存期间新编辑按顺序落库，数据库故障可重试，版本冲突停止覆盖', async ({ page }) => {
  const db = await databaseMock(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  await addImage(page)
  let release!: () => void
  db.delaySave(new Promise<void>((resolve) => { release = resolve }))
  await page.getByRole('textbox').fill('第一次编辑')
  await expect(page.getByLabel('画布保存状态')).toHaveText('正在保存…')
  await page.getByRole('textbox').fill('保存期间的第二次编辑')
  db.delaySave(); release()
  await saved(page)
  expect(db.canvas.snapshot.nodes[0]?.data.prompt).toBe('保存期间的第二次编辑')
  db.failSave(true)
  await page.getByRole('textbox').fill('断连时的草稿')
  await expect(page.getByLabel('画布保存状态')).toHaveText('保存失败')
  await expect(page.getByRole('textbox')).toHaveText('断连时的草稿')
  db.failSave(false)
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved(page)
  db.conflict()
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox').fill('冲突的草稿')
  await expect(page.getByLabel('画布保存状态')).toHaveText('保存冲突')
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(db.canvas.snapshot.nodes[0]?.data.prompt).toBe('断连时的草稿')
})

test('读取失败不创建空画布覆盖原数据，重连后正常恢复；失败任务可追踪', async ({ page }) => {
  const db = await databaseMock(page)
  await page.goto('/?canvas=33333333-3333-4333-8333-333333333333')
  const node = await addImage(page)
  await page.getByRole('textbox').fill('失败任务的输入')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => db.calls).toBe(1)
  await saved(page)
  db.complete('interrupted')
  db.failLoad(true)
  await page.reload()
  await expect(page.getByRole('button', { name: '重新连接' })).toBeVisible()
  expect(db.canvas.snapshot.nodes).toHaveLength(1)
  db.failLoad(false)
  await page.getByRole('button', { name: '重新连接' }).click()
  await saved(page)
  await node.click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('alert')).toContainText('未自动重新生成')
  expect(db.calls).toBe(1)
})

test('最新成功结果转存失败后再次生成失败，刷新不能将旧资源误认作新图已保存', async ({ page }) => {
  const key = `frame-space/dev/canvases/${canvasID}/generated/images/${targetID}/44444444-4444-4444-8444-444444444444.png`
  const input = { prompt: '新图', model, canvasId: canvasID, nodeId: targetID } as const
  const createdAt = new Date().toISOString()
  const latest: GenerationTask = { id: '66666666-6666-4666-8666-666666666666', input, status: 'failed', error: { code: 'MODEL_FAILED', message: '再次生成失败' }, createdAt, updatedAt: createdAt }
  const temporary: GenerationTask = { id: '77777777-7777-4777-8777-777777777777', input, status: 'succeeded', result: { url: resultURL, model, storageError: '新图片尚未保存，请及时下载。' }, createdAt, updatedAt: createdAt }
  const db = await databaseMock(page, {
    id: canvasID, version: 1, updatedAt: createdAt, tasks: [latest, temporary], results: [temporary],
    assets: [{ key, url: `/api/assets/content?key=${encodeURIComponent(key)}`, canvasId: canvasID, nodeId: targetID, kind: 'images', source: 'generated', contentType: 'image/png', bytes: 123 }],
    snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [{ id: targetID, position: { x: 200, y: 150 }, data: { kind: 'image', name: '图片节点 1', origin: 'generated', prompt: '下一次的草稿' } }] },
  })
  // /canvases provides only the latest task per node, while results are separate.
  await page.route(`**/api/canvases/${canvasID}`, (route) => route.request().method() === 'GET'
    ? route.fulfill({ json: { ...db.canvas, tasks: [latest] } }) : route.fallback())
  await page.route('**/tasks?latest=true', (route) => route.fulfill({ json: { tasks: [latest] } }))
  await page.goto(`/?canvas=${canvasID}`)
  const node = page.getByRole('article', { name: '图片节点 1', exact: true })
  await expect(node.locator('img')).toHaveAttribute('src', resultURL)
  await expect(node).toContainText('新图片尚未保存')
  await expect(node.getByText('已保存', { exact: true })).toHaveCount(0)
})
