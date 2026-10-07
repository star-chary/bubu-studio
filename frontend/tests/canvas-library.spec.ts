import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { CanvasSummary, GenerationTask, SavedCanvas } from '../src/api/persistence'

const firstID = '11111111-1111-4111-8111-111111111111'
const secondID = '22222222-2222-4222-8222-222222222222'
function emptyCanvas(id: string, title: string): SavedCanvas {
  return { id, title, version: 0, snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } }, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString() }
}
async function libraryMock(page: Page, initial: SavedCanvas[] = []) {
  const canvases = new Map(initial.map((canvas) => [canvas.id, canvas]))
  let failSave = false, failList = false, failCreate = false
  let saveDelay: Promise<void> | undefined
  const creates: string[] = []
  const taskReads: string[] = []
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', async (route) => {
    const request = route.request(), url = new URL(request.url())
    const id = url.pathname.split('/')[3] ?? ''
    if (request.method() === 'POST') {
      const body = request.postDataJSON()
      creates.push(body.id)
      if (!canvases.has(body.id)) canvases.set(body.id, emptyCanvas(body.id, body.title))
      // Simulate a lost response after the database already committed.
      if (failCreate) return route.abort()
      return route.fulfill({ status: 201, json: canvases.get(body.id) })
    }
    if (!id) {
      if (failList) return route.fulfill({ status: 503, json: { error: { message: '列表连接失败' } } })
      const offset = Number(url.searchParams.get('offset') ?? 0)
      const all = [...canvases.values()].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt) || b.id.localeCompare(a.id))
      const items: CanvasSummary[] = all.slice(offset, offset + 24).map((canvas) => ({ id: canvas.id, title: canvas.title!, updatedAt: canvas.updatedAt, nodeCount: canvas.snapshot.nodes.length, preview: canvas.snapshot.nodes.map((node) => ({ position: node.position, kind: node.data.kind })) }))
      return route.fulfill({ json: { canvases: items, hasMore: all.length > offset + 24 } })
    }
    const canvas = canvases.get(id)
    if (!canvas) return route.fulfill({ status: 404, json: { error: { message: '画布不存在' } } })
    if (url.pathname.endsWith('/tasks')) {
      taskReads.push(id)
      return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    }
    if (request.method() === 'PUT') {
      if (saveDelay) await saveDelay
      if (failSave) return route.fulfill({ status: 503, json: { error: { message: '保存连接失败' } } })
      const body = request.postDataJSON()
      if (body.version !== canvas.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '版本冲突' } } })
      canvas.snapshot = body.snapshot; canvas.version++; canvas.updatedAt = new Date().toISOString()
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  return { canvases, creates, taskReads, failSave(value: boolean) { failSave = value }, failList(value: boolean) { failList = value }, failCreate(value: boolean) { failCreate = value }, delaySave(value?: Promise<void>) { saveDelay = value } }
}
const saved = (page: Page) => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
async function create(page: Page, title: string) {
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByLabel('画布名称').fill(title)
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await saved(page)
  return new URL(page.url()).searchParams.get('canvas')!
}
async function editImage(page: Page, prompt: string) {
  await page.mouse.click(220, 190, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill(prompt)
}

test('首页列出旧画布，忽略旧的最后打开记录，不自动新建；窄屏与键盘可用', async ({ page }, info) => {
  const db = await libraryMock(page, [emptyCanvas(firstID, '以前的画布')])
  await page.addInitScript((id) => localStorage.setItem('frame-space:canvas', id), firstID)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  await expect(page.getByRole('link', { name: /打开画布：以前的画布/ })).toBeVisible()
  expect(db.creates).toHaveLength(0)
  await page.screenshot({ path: info.outputPath('library-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.locator('.home-page').evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('library-mobile.png') })
  await page.getByRole('button', { name: '新建画布', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByLabel('画布名称')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).not.toBeVisible()
})

test('新建两张画布独立编辑，返回前等待保存，重新打开和刷新恢复各自草稿与视口', async ({ page }) => {
  const db = await libraryMock(page)
  await page.goto('/')
  const a = await create(page, '分镜 A')
  await editImage(page, '只属于 A 的提示词')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  let release!: () => void
  db.delaySave(new Promise<void>((resolve) => { release = resolve }))
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('正在保存…')
  await expect(page).toHaveURL(new RegExp(a))
  db.delaySave(); release()
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  const aSnapshot = structuredClone(db.canvases.get(a)!.snapshot)
  const b = await create(page, '分镜 B')
  expect(a).not.toBe(b)
  await expect(page.getByRole('article')).toHaveCount(0)
  await expect(page.getByLabel('当前缩放比例')).toHaveText('100%')
  await editImage(page, '只属于 B 的提示词')
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await page.getByRole('link', { name: /打开画布：分镜 A/ }).click()
  await saved(page)
  await expect(page.getByRole('article')).toHaveCount(1)
  await expect(page.getByLabel('当前缩放比例')).not.toHaveText('100%')
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('只属于 A 的提示词')
  await page.reload()
  await saved(page)
  expect(db.canvases.get(a)!.snapshot).toEqual(aSnapshot)
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await page.getByRole('link', { name: /打开画布：分镜 B/ }).click()
  await saved(page)
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('只属于 B 的提示词')
})

test('保存失败时阻止返回并保留草稿，恢复后可以保存返回', async ({ page }) => {
  const db = await libraryMock(page, [emptyCanvas(firstID, '保存失败测试')])
  await page.goto(`/?canvas=${firstID}`)
  await saved(page)
  db.failSave(true)
  await editImage(page, '不能丢失的草稿')
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('已留在当前页面')
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('不能丢失的草稿')
  db.failSave(false)
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  expect(db.canvases.get(firstID)!.snapshot.nodes[0]!.data.prompt).toBe('不能丢失的草稿')
})

test('无效或不存在的地址不自动创建，持久画布服务不可用不降级成空白临时画布', async ({ page }) => {
  const db = await libraryMock(page)
  for (const id of ['invalid', firstID]) {
    await page.goto(`/?canvas=${id}`)
    const overlay = page.locator('.restore-overlay')
    await expect(overlay.getByRole('button', { name: '重新连接' })).toBeVisible()
    await overlay.getByRole('button', { name: '返回首页' }).click()
    await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  }
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: false } }))
  await page.goto(`/?canvas=${firstID}`)
  await expect(page.locator('.restore-overlay')).toContainText('保存服务未启用')
  expect(db.creates).toHaveLength(0)
})

test('列表失败可恢复，分页能找到旧画布，创建响应丢失后重试沿用同一个 ID', async ({ page }) => {
  const old = Array.from({ length: 25 }, (_, i) => emptyCanvas(`${String(i).padStart(8, '0')}-1111-4111-8111-111111111111`, `旧画布 ${i}`))
  const db = await libraryMock(page, old)
  db.failList(true)
  await page.goto('/')
  await expect(page.getByRole('alert')).toContainText('列表连接失败')
  await expect(page.getByText('还没有画布。', { exact: false })).not.toBeVisible()
  db.failList(false)
  await page.getByRole('button', { name: '重新加载' }).click()
  await expect(page.getByRole('link', { name: /打开画布/ })).toHaveCount(24)
  await page.getByRole('button', { name: '加载更多画布' }).click()
  await expect(page.getByRole('link', { name: /打开画布/ })).toHaveCount(25)
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByLabel('画布名称').fill('失败重试')
  db.failCreate(true)
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toBeVisible()
  db.failCreate(false)
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await saved(page)
  expect(db.creates).toHaveLength(2)
  expect(db.creates[0]).toBe(db.creates[1])
  expect(db.canvases.size).toBe(26)
})

test('任务记录和迟到的任务状态只属于原画布，切换后重新打开可继续查询', async ({ page }) => {
  const a = emptyCanvas(firstID, '任务画布 A'), b = emptyCanvas(secondID, '任务画布 B')
  a.snapshot.nodes = [{ id: firstID, position: { x: 220, y: 190 }, data: { kind: 'image', name: '任务节点', prompt: 'A 的草稿' } }]
  const task: GenerationTask = { id: secondID, input: { canvasId: firstID, nodeId: firstID, prompt: 'A 提交的任务', model: 'doubao-seedream-5-0-260128' }, status: 'running', createdAt: a.updatedAt, updatedAt: a.updatedAt }
  a.tasks = [task]
  const db = await libraryMock(page, [a, b])
  await page.goto(`/?canvas=${firstID}`)
  await saved(page)
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await page.getByRole('link', { name: /打开画布：任务画布 B/ }).click()
  await saved(page)
  const previousReads = db.taskReads.filter((id) => id === firstID).length
  task.status = 'failed'; task.error = { code: 'TEST', message: 'A 的任务失败' }
  await page.getByRole('button', { name: '任务记录' }).click()
  await expect(page.getByText('还没有生成任务')).toBeVisible()
  await expect(page.getByRole('article')).toHaveCount(0)
  expect(db.taskReads.filter((id) => id === firstID)).toHaveLength(previousReads)
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await page.getByRole('link', { name: /打开画布：任务画布 A/ }).click()
  await saved(page)
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('alert')).toContainText('A 的任务失败')
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('A 的草稿')
})

test('离开临时画布先提示，可取消并保留编辑，明确离开才返回首页', async ({ page }) => {
  await libraryMock(page)
  await page.goto(`/?temporary=${firstID}`)
  await editImage(page, '临时内容')
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '还有内容未保存' })).toBeVisible()
  await page.getByRole('button', { name: '继续编辑' }).click()
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('临时内容')
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await page.getByRole('button', { name: '仍然返回首页' }).click()
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
})
