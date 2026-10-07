import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { GenerationTask, SavedCanvas } from '../src/api/persistence'
import type { StoredAsset } from '../src/api/assets'

const canvasID = '33333333-3333-4333-8333-333333333333'
const teaID = '11111111-1111-4111-8111-111111111111'
const skyID = '22222222-2222-4222-8222-222222222222'
const videoID = '44444444-4444-4444-8444-444444444444'
const targetID = '55555555-5555-4555-8555-555555555555'
const imageBytes = readFileSync(fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)))
const videoBytes = readFileSync(fileURLToPath(new URL('./fixtures/reference-video.mp4', import.meta.url)))
const editor = (page: Page) => page.getByRole('textbox', { name: /^(图片|视频)提示词$/ })
const popup = (page: Page) => page.getByRole('dialog', { name: '素材引用菜单', exact: true })
const chips = (page: Page) => editor(page).locator('[data-prompt-reference]')

async function setup(page: Page, kind: 'image' | 'video' = 'image') {
  const assets: StoredAsset[] = [teaID, skyID, videoID].map((id) => ({
    canvasId: canvasID, nodeId: id, key: `${id}.${id === videoID ? 'mp4' : 'png'}`, url: `/api/assets/content?key=${id}.${id === videoID ? 'mp4' : 'png'}`,
    kind: id === videoID ? 'videos' : 'images', source: 'uploads', contentType: id === videoID ? 'video/mp4' : 'image/png', bytes: 1000,
    media: { width: 960, height: 540, ...(id === videoID ? { durationSeconds: 2, frameRate: 24, videoCodec: 'h264' } : {}) }, videoReference: { eligible: true },
  }))
  const canvas: SavedCanvas = { id: canvasID, version: 1, assets, tasks: [], results: [], updatedAt: new Date().toISOString(), snapshot: { viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
    { id: teaID, position: { x: 40, y: 145 }, data: { kind: 'image', name: '茶叶.png', origin: 'upload' } },
    { id: skyID, position: { x: 545, y: 145 }, data: { kind: 'image', name: '天空.png', origin: 'upload' } },
    { id: videoID, position: { x: 40, y: 515 }, data: { kind: 'video', name: '镜头.mp4', origin: 'upload' } },
    { id: targetID, position: { x: 1050, y: 150 }, data: { kind, name: '创作节点', prompt: '' } },
  ] } }
  let generationCalls = 0
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill({ contentType: route.request().url().includes('mp4') ? 'video/mp4' : 'image/png', body: route.request().url().includes('mp4') ? videoBytes : imageBytes }))
  await page.route('**/api/canvases**', (route) => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      if (body.version !== canvas.version) return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT' } } })
      canvas.snapshot = body.snapshot; canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.route('**/api/images/generations', (route) => { generationCalls++; return route.fulfill({ status: 400, json: { error: { message: 'test generation blocked' } } }) })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByRole('article', { name: '茶叶.png', exact: true })).toContainText('960 × 540')
  await page.getByRole('article', { name: '创作节点', exact: true }).click({ position: { x: 65, y: 75 } })
  return { canvas, calls: () => generationCalls, target: () => canvas.snapshot.nodes.find((node) => node.id === targetID)!.data }
}
async function mention(page: Page, name: string, kind: '图片' | '视频' = '图片') {
  await editor(page).press('End')
  await editor(page).pressSequentially('@')
  await popup(page).getByRole('button', { name: `${kind}素材`, exact: true }).click()
  await popup(page).locator('.mention-submenu').getByRole('button', { name: `引用 ${name}`, exact: true }).click()
  await expect(popup(page)).toHaveCount(0)
}
async function save(page: Page) {
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
}
async function reopen(page: Page) { await page.getByRole('article', { name: '创作节点', exact: true }).click({ position: { x: 65, y: 75 } }) }

test('素材选择同步标签、参考和连线；重复提及不重复引用，刷新恢复稳定身份', async ({ page }, info) => {
  const db = await setup(page)
  await editor(page).fill('参考 ')
  await mention(page, '茶叶.png')
  await expect(chips(page)).toHaveText(['图片1'])
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  await editor(page).pressSequentially('中的颜色，结合 ')
  await mention(page, '天空.png')
  await editor(page).pressSequentially('@')
  await popup(page).locator('.mention-main').getByRole('button', { name: '引用 茶叶.png', exact: true }).click()
  await expect(chips(page)).toHaveText(['图片1', '图片2', '图片1'])
  await expect(page.locator('.prompt-reference')).toHaveCount(2)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  await save(page)
  expect(db.target().referenceIds).toEqual([teaID, skyID])
  expect(db.target().promptParts?.filter((part) => part.type === 'reference').map((part) => part.nodeId)).toEqual([teaID, skyID, teaID])
  expect(JSON.stringify(db.target())).not.toMatch(/blob:|<span|<img/)
  await page.reload(); await reopen(page)
  await expect(chips(page)).toHaveText(['图片1', '图片2', '图片1'])
  await page.screenshot({ path: info.outputPath('mentions-restored.png') })
  expect(db.calls()).toBe(0)
})

test('移除参考后的标签失效且不串号；删除源节点可保存恢复；删除标签不删除参考', async ({ page }) => {
  const db = await setup(page)
  await mention(page, '茶叶.png'); await mention(page, '天空.png')
  await page.getByRole('button', { name: '移除参考 茶叶.png', exact: true }).click()
  await expect(chips(page).nth(0)).toHaveClass(/is-invalid/)
  await expect(chips(page).nth(1)).toHaveText('图片1')
  await expect(chips(page).nth(1)).toHaveAttribute('data-node-id', skyID)
  await mention(page, '茶叶.png')
  await expect(chips(page)).toHaveText(['图片2', '图片1', '图片2'])
  await editor(page).press('Control+End'); await editor(page).press('Backspace'); await editor(page).press('Backspace')
  await expect(chips(page)).toHaveCount(2)
  await expect(page.locator('.prompt-reference')).toHaveCount(2)
  await save(page)
  db.canvas.snapshot.nodes = db.canvas.snapshot.nodes.filter((node) => node.id !== teaID)
  db.target().referenceIds = [skyID]
  db.canvas.version++
  await page.reload(); await reopen(page)
  await expect(chips(page).nth(0)).toHaveClass(/is-invalid/)
  await expect(chips(page).nth(0)).toHaveAttribute('title', /来源素材已不存在/)
  await editor(page).press('Control+End'); await editor(page).pressSequentially('继续编辑')
  await save(page)
  expect(db.target().promptParts?.some((part) => part.type === 'reference' && part.nodeId === teaID)).toBe(true)
})

test('菜单搜索、两级键盘操作、Esc 优先关闭菜单，图片节点禁用视频素材', async ({ page }, info) => {
  await setup(page)
  await editor(page).fill('@')
  await expect(popup(page).getByRole('button', { name: '图片素材', exact: true })).toBeVisible()
  await expect(popup(page).getByRole('button', { name: '音频素材', exact: true })).toHaveCount(0)
  await editor(page).press('ArrowDown'); await editor(page).press('ArrowRight')
  const video = popup(page).getByRole('button', { name: '引用 镜头.mp4', exact: true })
  await expect(video).toHaveAttribute('aria-disabled', 'true')
  await expect(video).toContainText('图片节点只能参考图片')
  await editor(page).press('ArrowLeft'); await editor(page).press('ArrowRight')
  await popup(page).getByRole('textbox', { name: '搜索素材' }).fill('天空')
  await expect(popup(page).locator('.mention-submenu').getByRole('button')).toHaveCount(1)
  await page.screenshot({ path: info.outputPath('mention-menu.png') })
  await popup(page).getByRole('textbox').press('Enter')
  await expect(chips(page)).toHaveText(['图片1'])
  await editor(page).pressSequentially('@')
  await editor(page).press('Escape')
  await expect(popup(page)).toHaveCount(0)
  await expect(editor(page)).toBeVisible()
  await editor(page).press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('标签撤销重做、整块删除、纯文本粘贴与中文组词不破坏编辑', async ({ page }) => {
  await setup(page)
  await editor(page).fill('前文 ')
  await mention(page, '茶叶.png')
  await editor(page).press('Control+z')
  await expect(chips(page)).toHaveCount(0)
  await expect(editor(page)).toHaveText('前文 @')
  await editor(page).press('Control+Shift+z')
  await expect(chips(page)).toHaveCount(1)
  await editor(page).press('Enter'); await editor(page).pressSequentially('后文')
  await editor(page).evaluate((element) => {
    const data = new DataTransfer(); data.setData('text/plain', '<img src=x onerror=alert(1)>\n普通文本')
    element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
  })
  await expect(editor(page)).toContainText('<img src=x onerror=alert(1)>')
  await expect(editor(page).locator('img')).toHaveCount(1)
  await editor(page).fill('')
  await editor(page).dispatchEvent('compositionstart')
  await editor(page).evaluate((element) => { element.textContent = '@'; element.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true })) })
  await expect(popup(page)).toHaveCount(0)
  await editor(page).press('Control+End')
  await editor(page).dispatchEvent('compositionend')
  await expect(popup(page)).toBeVisible()
  await editor(page).press('Escape'); await editor(page).press('Control+a'); await editor(page).press('Backspace')
  await expect(editor(page)).toBeEmpty()
})

test('视频节点可引用两类资源；窄屏菜单不越界，放大恢复不丢标签', async ({ page }, info) => {
  await setup(page, 'video')
  await mention(page, '茶叶.png'); await mention(page, '镜头.mp4', '视频')
  await expect(chips(page)).toHaveText(['图片1', '视频1'])
  await page.getByRole('button', { name: '放大输入框', exact: true }).click()
  await page.setViewportSize({ width: 375, height: 812 })
  await editor(page).press('Control+End'); await editor(page).pressSequentially('@')
  await popup(page).getByRole('button', { name: '视频素材', exact: true }).click()
  await expect(popup(page).getByRole('button', { name: '返回素材分类' })).toBeVisible()
  const box = (await popup(page).boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(12); expect(box.y).toBeGreaterThanOrEqual(12)
  expect(box.x + box.width).toBeLessThanOrEqual(363); expect(box.y + box.height).toBeLessThanOrEqual(800)
  await page.screenshot({ path: info.outputPath('mentions-mobile.png') })
  await popup(page).getByRole('button', { name: '引用 镜头.mp4', exact: true }).click()
  await page.getByRole('button', { name: '恢复输入框大小', exact: true }).click()
  await expect(chips(page)).toHaveText(['图片1', '视频1', '视频1'])
  await expect(page.locator('.prompt-reference')).toHaveCount(2)
})

test('删除全部标签后原有图片生成入口恢复，未保存素材不能新增引用', async ({ page }) => {
  const db = await setup(page)
  await mention(page, '茶叶.png')
  await editor(page).fill('普通提示词')
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeEnabled()
  await save(page)
  expect(db.target().promptParts).toBeUndefined()
  await reopen(page)
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(db.calls).toBe(1)
  db.canvas.assets = db.canvas.assets.filter((asset) => asset.nodeId !== skyID)
  await page.reload(); await reopen(page)
  await editor(page).fill('@')
  await popup(page).getByRole('button', { name: '图片素材', exact: true }).click()
  // An upload that has no durable asset after reload is not a resource candidate.
  await expect(popup(page).getByRole('button', { name: '引用 天空.png', exact: true })).toHaveCount(0)
})

test('多行文字的换行与光标位置保存准确，可在中间插入标签', async ({ page }) => {
  const db = await setup(page)
  await editor(page).fill('开头')
  await editor(page).press('Enter'); await editor(page).pressSequentially('后文')
  await save(page)
  expect(db.target().prompt).toBe('开头\n后文')
  await reopen(page)
  await editor(page).fill('第一行\n第二行\n第三行')
  await editor(page).press('Control+Home'); await editor(page).press('ArrowDown'); await editor(page).press('Home')
  await editor(page).pressSequentially('@')
  await popup(page).getByRole('button', { name: '图片素材', exact: true }).click()
  await popup(page).getByRole('button', { name: '引用 茶叶.png', exact: true }).click()
  await save(page)
  expect(db.target().prompt).toBe('第一行\n@茶叶.png 第二行\n第三行')
  await page.reload(); await reopen(page)
  await expect(editor(page)).toHaveText('第一行\n图片1 第二行\n第三行', { useInnerText: true })
})


test('带标签图片提交固定片段和资源顺序，重复提及不重复附件', async ({ page }) => {
  const db = await setup(page)
  await mention(page, '天空.png'); await mention(page, '茶叶.png'); await mention(page, '天空.png')
  let input: any
  await page.route('**/api/images/generations', async (route) => {
    input = route.request().postDataJSON()
    await route.fulfill({ status: 422, json: { error: { message: '模拟结果，不调用模型' } } })
  })
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await expect.poll(() => input).toBeTruthy()
  expect(input.referenceKeys).toEqual([`${skyID}.png`, `${teaID}.png`])
  expect(input.promptParts.filter((p: any) => p.type === 'reference').map((p: any) => p.nodeId)).toEqual([skyID, teaID, skyID])
  expect(db.target().referenceIds).toEqual([skyID, teaID])
  expect(input.prompt).toContain('@天空.png')
  await page.getByRole('button', { name: '移除参考 天空.png', exact: true }).click()
  await expect(page.getByRole('button', { name: '生成图片', exact: true })).toBeDisabled()
})

test('视频参数和片段提交、同状态进度更新、刷新恢复与只重试保存', async ({ page }, info) => {
  const db = await setup(page, 'video')
  await mention(page, '茶叶.png'); await mention(page, '镜头.mp4', '视频')
  await page.getByRole('button', { name: /^视频参数，/ }).click()
  await page.getByRole('radio', { name: '9:16', exact: true }).check()
  await page.getByLabel('生成声音', { exact: true }).uncheck()
  await page.keyboard.press('Escape')
  let task: GenerationTask | undefined
  let calls = 0, retries = 0
  await page.route('**/api/canvases/*/tasks?*', (route) => route.fulfill({ json: { tasks: task ? [task] : [], hasMore: false } }))
  await page.route('**/api/videos/generations', (route) => {
    calls++
    const { taskId, ...input } = route.request().postDataJSON()
    const now = new Date().toISOString()
    task = { id: taskId, kind: 'video', input, status: 'running', providerStatus: 'queued', providerTaskId: 'cgt-test', createdAt: now, updatedAt: now }
    return route.fulfill({ status: 202, json: task })
  })
  await page.getByRole('button', { name: '生成视频', exact: true }).click()
  await expect.poll(() => task?.input).toBeTruthy()
  expect(task!.input.duration).toBe(4); expect(task!.input.resolution).toBe('480p')
  expect(task!.input.ratio).toBe('9:16'); expect(task!.input.generateAudio).toBe(false)
  expect(task!.input.references?.map((r) => [r.kind, r.nodeId])).toEqual([['image', teaID], ['video', videoID]])
  await expect(page.getByRole('button', { name: '模型排队中…', exact: true })).toBeDisabled()
  task = { ...task!, providerStatus: 'running', pollingError: { code: 'WAIT', message: '临时查询失败，继续查询原任务' }, updatedAt: new Date(Date.now()+1000).toISOString() }
  await expect(page.getByText('临时查询失败，继续查询原任务', { exact: true })).toBeVisible({ timeout: 6000 })
  db.canvas.tasks = [task]
  await page.reload(); await reopen(page)
  await page.getByRole('button', { name: /^视频参数，/ }).click()
  await expect(page.getByRole('radio', { name: '9:16', exact: true })).toBeChecked()
  await expect(page.getByLabel('生成声音', { exact: true })).not.toBeChecked()
  await page.keyboard.press('Escape')
  task = { ...task, status: 'storage_failed', pollingError: undefined, error: { code: 'VIDEO_STORAGE_FAILED', message: '视频已生成但转存失败' }, updatedAt: new Date(Date.now()+2000).toISOString() }
  await expect(page.getByRole('button', { name: '重试保存视频', exact: true })).toBeVisible({ timeout: 6000 })
  await page.route('**/api/tasks/*/storage-retries', (route) => {
    retries++; expect(route.request().postDataJSON()).toEqual({})
    task = { ...task!, status: 'saving', error: undefined, updatedAt: new Date(Date.now()+3000).toISOString() }
    return route.fulfill({ status: 202, json: task })
  })
  await page.getByRole('button', { name: '重试保存视频', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存中…', exact: true })).toBeDisabled()
  expect(calls).toBe(1); expect(retries).toBe(1)
  await page.screenshot({ path: info.outputPath('video-generation-controls.png') })
})
