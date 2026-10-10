import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page, Route } from '@playwright/test'
import { test, expect } from './setup'
import type { CanvasSnapshot, GenerationTask, SavedCanvas } from '../src/api/persistence'
import type { StoredAsset } from '../src/api/assets'

const canvasID = '33333333-3333-4333-8333-333333333333'
const sourceID = '11111111-1111-4111-8111-111111111111'
const targetID = '22222222-2222-4222-8222-222222222222'
const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const model = 'doubao-seedream-5-0-260128'
const article = (page: Page, name: string) => page.getByRole('article', { name, exact: true })
const menu = (page: Page) => page.getByRole('menu', { name: '节点菜单', exact: true })
const saved = (page: Page) => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
const emptyNode = (id: string, kind: 'image' | 'video', x: number): CanvasSnapshot['nodes'][number] => ({
  id, position: { x, y: 150 }, data: { kind, name: kind === 'image' ? '图片节点' : '视频节点' },
})
const assetFor = (nodeId: string): StoredAsset => ({
  canvasId: canvasID, nodeId, key: `${nodeId}.png`, url: `/api/assets/content?key=${nodeId}.png`,
  kind: 'images', source: 'uploads', contentType: 'image/png', bytes: imageBytes.length,
})

async function setup(page: Page, options: { nodes?: CanvasSnapshot['nodes']; assets?: StoredAsset[]; tasks?: GenerationTask[] } = {}) {
  const canvas: SavedCanvas = { id: canvasID, title: '删除专项测试', version: 1, updatedAt: new Date().toISOString(),
    snapshot: { nodes: options.nodes ?? [emptyNode(sourceID, 'image', 60), emptyNode(targetID, 'video', 820)], viewport: { x: 0, y: 0, zoom: 1 } },
    assets: options.assets ?? [], tasks: options.tasks ?? [], results: [],
  }
  let failSave = false
  let saveGate: Promise<void> | undefined
  let saveStarted = 0
  let taskPolls = 0
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: 'image/png', body: imageBytes }))
  await page.route('**/api/canvases**', async (route) => {
    if (new URL(route.request().url()).pathname.endsWith('/tasks')) {
      taskPolls++
      return route.fulfill({ json: { tasks: canvas.tasks, hasMore: false } })
    }
    if (route.request().method() === 'PUT') {
      saveStarted++
      if (saveGate) await saveGate
      if (failSave) return route.fulfill({ status: 503, json: { error: { code: 'DATABASE_UNAVAILABLE', message: '删除暂未保存，请重试' } } })
      const body = route.request().postDataJSON()
      if (body.version !== canvas.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '版本冲突' } } })
      canvas.snapshot = body.snapshot
      canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: { ...canvas, results: canvas.tasks.filter((task) => task.status === 'succeeded') } })
  })
  await page.goto(`/?canvas=${canvasID}`)
  await saved(page)
  return { canvas, errors, failSave(value: boolean) { failSave = value }, holdSave(value?: Promise<void>) { saveGate = value },
    get saveStarted() { return saveStarted }, get taskPolls() { return taskPolls } }
}

async function rightClick(page: Page, name: string) {
  await article(page, name).click({ button: 'right' })
  await expect(menu(page)).toBeVisible()
}

async function deleteByMenu(page: Page, name: string) {
  await rightClick(page, name)
  await menu(page).getByRole('menuitem', { name: '删除', exact: true }).click()
  await expect(article(page, name)).toHaveCount(0)
}

test('右键删除只作用于当前节点，复用画布菜单样式并支持关闭和键盘确认', async ({ page }, info) => {
  const db = await setup(page)
  await page.mouse.click(600, 650, { button: 'right' })
  const canvasStyle = await page.getByRole('menu', { name: '画布菜单', exact: true }).evaluate((element) => {
    const style = getComputedStyle(element)
    return { width: style.width, background: style.backgroundColor, borderRadius: style.borderRadius }
  })
  await rightClick(page, '图片节点')
  await expect(menu(page).getByRole('menuitem')).toHaveCount(1)
  await expect(menu(page).getByRole('menuitem', { name: '删除', exact: true })).toHaveAttribute('aria-keyshortcuts', 'Delete')
  expect(await menu(page).evaluate((element) => {
    const style = getComputedStyle(element)
    return { width: style.width, background: style.backgroundColor, borderRadius: style.borderRadius }
  })).toEqual(canvasStyle)
  await page.screenshot({ path: info.outputPath('node-delete-menu.png') })
  await page.keyboard.press('Escape')
  await expect(menu(page)).toHaveCount(0)
  await expect(page.getByRole('article')).toHaveCount(2)
  await rightClick(page, '图片节点')
  await page.mouse.click(600, 650)
  await expect(menu(page)).toHaveCount(0)
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  await rightClick(page, '视频节点')
  await expect(page.getByRole('dialog', { name: '图片提示词', exact: true })).toHaveCount(0)
  await page.keyboard.press('ArrowDown')
  await expect(menu(page).getByRole('menuitem')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(article(page, '视频节点')).toHaveCount(0)
  await expect(article(page, '图片节点')).toBeVisible()
  await expect(menu(page)).toHaveCount(0)
  await saved(page)
  expect(db.canvas.snapshot.nodes.map((node) => node.id)).toEqual([sourceID])
  expect(db.errors).toEqual([])
})

test('选中图片或视频后 Delete 删除节点并关闭面板，菜单内也可直接按 Delete', async ({ page }) => {
  const db = await setup(page)
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('main')).toBeFocused()
  await expect(page.getByRole('dialog', { name: '图片提示词', exact: true })).toBeVisible()
  await page.keyboard.press('Backspace')
  await expect(article(page, '图片节点')).toBeVisible()
  await page.keyboard.press('Delete')
  await expect(article(page, '图片节点')).toHaveCount(0)
  await expect(page.getByRole('dialog', { name: '图片提示词', exact: true })).toHaveCount(0)
  await rightClick(page, '视频节点')
  await page.keyboard.press('Delete')
  await expect(page.getByRole('article')).toHaveCount(0)
  await expect(menu(page)).toHaveCount(0)
  await expect(page.locator('.empty-guide')).toBeVisible()
  await saved(page)
  expect(db.canvas.snapshot.nodes).toEqual([])
  expect(db.errors).toEqual([])
})

test('无选中项、空白处右键、组合键、输入法组词和长按不误删节点', async ({ page }) => {
  await setup(page)
  await page.keyboard.press('Delete')
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  for (const key of ['Control+Delete', 'Meta+Delete', 'Alt+Delete', 'Shift+Delete']) await page.keyboard.press(key)
  await page.getByRole('main').dispatchEvent('keydown', { key: 'Delete', isComposing: true })
  await page.getByRole('main').dispatchEvent('keydown', { key: 'Delete', repeat: true })
  await expect(page.getByRole('article')).toHaveCount(2)
  await page.mouse.click(600, 650)
  await page.keyboard.press('Delete')
  await expect(page.getByRole('article')).toHaveCount(2)
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  await page.mouse.click(600, 650, { button: 'right' })
  await page.keyboard.press('Delete')
  await expect(page.getByRole('article')).toHaveCount(2)
})

test('提示词内 Delete 只删文字，参数输入与素材搜索中不会删除节点', async ({ page }) => {
  await setup(page)
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  const editor = page.getByRole('textbox', { name: '图片提示词', exact: true })
  await editor.fill('ABC')
  await editor.press('Home')
  await editor.press('Delete')
  await expect(editor).toHaveText('BC')
  await expect(article(page, '图片节点')).toBeVisible()
  await editor.fill('@')
  await page.getByRole('dialog', { name: '素材引用菜单', exact: true }).getByRole('button', { name: '图片素材', exact: true }).click()
  const search = page.getByRole('dialog', { name: '素材引用菜单', exact: true }).getByRole('textbox')
  await search.fill('ABC')
  await search.press('Home')
  await search.press('Delete')
  await expect(search).toHaveValue('BC')
  await expect(article(page, '图片节点')).toBeVisible()
  await article(page, '视频节点').click({ position: { x: 60, y: 70 } })
  await page.getByRole('button', { name: /^视频参数，/ }).click()
  const duration = page.getByRole('spinbutton', { name: '视频时长秒数', exact: true })
  await duration.press('Delete')
  await expect(article(page, '视频节点')).toBeVisible()
  await page.keyboard.press('Escape')
  await article(page, '视频节点').click({ position: { x: 60, y: 70 } })
  await page.keyboard.press('Delete')
  await expect(article(page, '视频节点')).toHaveCount(0)
  await expect(article(page, '图片节点')).toBeVisible()
})

test('参考选择模式和离开确认框内 Delete 不会误删节点', async ({ page }) => {
  await setup(page)
  await article(page, '图片节点').click({ position: { x: 60, y: 70 } })
  await page.getByRole('button', { name: '参考', exact: true }).click()
  await article(page, '视频节点').click({ position: { x: 60, y: 70 } })
  await page.keyboard.press('Delete')
  await article(page, '图片节点').click({ button: 'right', position: { x: 60, y: 70 } })
  await expect(menu(page)).toHaveCount(0)
  await expect(page.getByRole('article')).toHaveCount(2)
  await page.keyboard.press('Escape')
  await page.goto(`/?temporary=${canvasID}`)
  await page.mouse.click(60, 150, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await article(page, '图片节点 1').click({ position: { x: 60, y: 70 } })
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '还有内容未保存', exact: true })).toBeVisible()
  await page.keyboard.press('Delete')
  await expect(article(page, '图片节点 1')).toHaveCount(1)
})

test('删除菜单在窄屏边缘和窗口缩放后不越界', async ({ page }) => {
  await setup(page)
  await page.setViewportSize({ width: 375, height: 480 })
  await page.mouse.click(370, 440, { button: 'right' })
  await expect(menu(page)).toBeVisible()
  for (const size of [{ width: 375, height: 480 }, { width: 320, height: 400 }]) {
    await page.setViewportSize(size)
    await expect.poll(async () => {
      const box = await menu(page).boundingBox()
      return !!box && box.x >= 8 && box.y >= 8 && box.x + box.width <= size.width - 8 && box.y + box.height <= size.height - 8
    }).toBe(true)
  }
  await menu(page).getByRole('menuitem', { name: '删除', exact: true }).click()
  await expect(article(page, '图片节点')).toHaveCount(0)
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(article(page, '视频节点')).toBeVisible()
})

async function linkedCanvas(page: Page) {
  const source = emptyNode(sourceID, 'image', 60)
  source.data = { ...source.data, name: '来源图片', origin: 'upload' }
  const target = emptyNode(targetID, 'image', 820)
  target.data = { ...target.data, name: '目标图片', referenceIds: [sourceID], prompt: '参考 @来源图片 的颜色',
    promptParts: [{ type: 'text', text: '参考 ' }, { type: 'reference', nodeId: sourceID, kind: 'image', name: '来源图片' }, { type: 'text', text: ' 的颜色' }] }
  const db = await setup(page, { nodes: [source, target], assets: [assetFor(sourceID)] })
  await expect(article(page, '来源图片')).toContainText('960 × 540')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  return db
}

test('删除来源节点清理引用和连线，保留失效标签提示，自动保存后刷新不恢复', async ({ page }) => {
  const db = await linkedCanvas(page)
  await deleteByMenu(page, '来源图片')
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await saved(page)
  expect(db.canvas.snapshot.nodes).toHaveLength(1)
  expect(db.canvas.snapshot.nodes[0]!.data.referenceIds).toEqual([])
  await article(page, '目标图片').click({ position: { x: 60, y: 70 } })
  await expect(page.locator('.prompt-reference')).toHaveCount(0)
  await expect(page.locator('.prompt-chip.is-invalid')).toHaveCount(1)
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
  await page.reload()
  await saved(page)
  await expect(article(page, '来源图片')).toHaveCount(0)
  await expect(article(page, '目标图片')).toBeVisible()
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await article(page, '目标图片').click({ position: { x: 60, y: 70 } })
  await expect(page.locator('.prompt-chip.is-invalid')).toHaveCount(1)
  expect(db.errors).toEqual([])
})

test('删除引用目标只移除目标及连线，来源素材保留并可刷新恢复', async ({ page }) => {
  const db = await linkedCanvas(page)
  await article(page, '目标图片').click({ position: { x: 60, y: 70 } })
  await page.keyboard.press('Delete')
  await expect(article(page, '目标图片')).toHaveCount(0)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await expect(article(page, '来源图片').getByRole('img')).toBeVisible()
  await saved(page)
  await page.reload()
  await saved(page)
  expect(db.canvas.snapshot.nodes.map((node) => node.id)).toEqual([sourceID])
  await expect(article(page, '来源图片')).toContainText('960 × 540')
  expect(db.errors).toEqual([])
})

test('删除最后节点遇到保存失败时显示失败，重试保存空画布后刷新不恢复', async ({ page }) => {
  const db = await setup(page, { nodes: [emptyNode(sourceID, 'image', 60)] })
  db.failSave(true)
  await deleteByMenu(page, '图片节点')
  await expect(page.getByLabel('画布保存状态')).toHaveText('保存失败')
  await expect(page.getByRole('alert')).toContainText('删除暂未保存，请重试')
  expect(db.canvas.snapshot.nodes).toHaveLength(1)
  db.failSave(false)
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved(page)
  expect(db.canvas.snapshot.nodes).toEqual([])
  await page.reload()
  await saved(page)
  await expect(page.getByRole('article')).toHaveCount(0)
  await expect(page.locator('.empty-guide')).toBeVisible()
})

test('删除生成中的节点后迟到结果不会重建节点，任务及结果仍在任务记录中', async ({ page }) => {
  const task: GenerationTask = { id: '44444444-4444-4444-8444-444444444444', status: 'running', kind: 'image',
    input: { canvasId: canvasID, nodeId: sourceID, model, prompt: '删除后仍保留的任务' },
    createdAt: '2026-10-10T00:00:00Z', updatedAt: '2026-10-10T00:00:00Z' }
  const db = await setup(page, { tasks: [task] })
  await expect(article(page, '图片节点').getByRole('status')).toContainText('图片生成中')
  await deleteByMenu(page, '图片节点')
  await saved(page)
  task.status = 'succeeded'
  task.updatedAt = '2026-10-10T00:01:00Z'
  task.result = { url: assetFor(sourceID).url, model, asset: assetFor(sourceID) }
  const polls = db.taskPolls
  await expect.poll(() => db.taskPolls).toBeGreaterThan(polls)
  await expect(article(page, '图片节点')).toHaveCount(0)
  await page.getByRole('button', { name: '任务记录', exact: true }).click()
  await expect(page.getByRole('complementary', { name: '生成任务记录', exact: true })).toContainText('已完成')
  await expect(page.getByRole('link', { name: '查看结果', exact: true })).toHaveAttribute('href', task.result.url!)
  await page.reload()
  await saved(page)
  await expect(article(page, '图片节点')).toHaveCount(0)
  await expect(article(page, '视频节点')).toBeVisible()
  expect(db.errors).toEqual([])
})

test('删除排队及上传中的素材会释放预览和取消上传，迟到上传结果不重建节点', async ({ page }) => {
  const requests: { route: Route; nodeId: string }[] = []
  const aborted: string[] = []
  page.on('requestfailed', (request) => { if (request.url().includes('/api/assets?')) aborted.push(new URL(request.url()).searchParams.get('nodeId')!) })
  await page.route('**/api/storage/config', (route) => route.fulfill({ json: { enabled: true, maxImageBytes: 20 << 20, maxVideoBytes: 200 << 20, maxAudioBytes: 15_000_000 } }))
  await page.route('**/api/assets?*', (route) => { requests.push({ route, nodeId: new URL(route.request().url()).searchParams.get('nodeId')! }) })
  const db = await setup(page, { nodes: [] })
  await page.mouse.click(60, 150, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(['first.png', 'second.png', 'queued.png'].map((name) => ({ name, mimeType: 'image/png', buffer: imageBytes })))
  await expect.poll(() => requests.length).toBe(2)
  const queuedURL = await article(page, 'queued.png').getByRole('img').getAttribute('src')
  const firstURL = await article(page, 'first.png').getByRole('img').getAttribute('src')
  await deleteByMenu(page, 'queued.png')
  await deleteByMenu(page, 'first.png')
  await expect.poll(() => aborted.length).toBe(1)
  for (const url of [queuedURL!, firstURL!]) {
    expect(await page.evaluate(async (url) => { try { return (await fetch(url)).ok } catch { return false } }, url)).toBe(false)
  }
  for (const request of requests) await request.route.fulfill({ status: 201, json: assetFor(request.nodeId) })
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(0)
  await saved(page)
  expect(requests).toHaveLength(2)
  expect(db.canvas.snapshot.nodes.map((node) => node.data.name)).toEqual(['second.png'])
  await deleteByMenu(page, 'second.png')
  await saved(page)
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page).toHaveURL('/')
  expect(db.errors).toEqual([])
})

for (const kind of ['image', 'video'] as const) {
  test(`删除仍在等待保存的${kind === 'image' ? '图片' : '视频'}节点不会继续提交生成请求`, async ({ page }) => {
    const node = emptyNode(sourceID, kind, 60)
    if (kind === 'video') node.data.videoMode = 'text'
    const db = await setup(page, { nodes: [node] })
    let release!: () => void
    let calls = 0
    await page.route(`**/api/${kind}s/generations`, (route) => { calls++; return route.fulfill({ status: 400, json: { error: { message: '不应提交' } } }) })
    await article(page, node.data.name).click({ position: { x: 60, y: 70 } })
    const editor = page.getByRole('textbox', { name: kind === 'image' ? '图片提示词' : '视频提示词', exact: true })
    await editor.fill('保存期间删除的节点')
    const generate = page.getByRole('button', { name: kind === 'image' ? '生成图片' : '生成视频', exact: true })
    await expect(generate).toBeEnabled()
    const before = db.saveStarted
    db.holdSave(new Promise<void>((resolve) => { release = resolve }))
    try {
      // 视口变化需要保存，但不改变已经取得报价的生成输入。
      await page.mouse.move(650, 300)
      await page.mouse.wheel(0, -160)
      await expect(page.getByLabel('当前缩放比例')).not.toHaveText('100%')
      await generate.click()
      await expect.poll(() => db.saveStarted).toBeGreaterThan(before)
      await deleteByMenu(page, node.data.name)
    } finally { db.holdSave(); release() }
    await saved(page)
    await page.getByRole('button', { name: '返回首页', exact: true }).click()
    await expect(page).toHaveURL('/')
    expect(calls).toBe(0)
    expect(db.canvas.snapshot.nodes).toEqual([])
    expect(db.errors).toEqual([])
  })
}
