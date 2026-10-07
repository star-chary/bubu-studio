import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from './setup'
import type { SavedCanvas } from '../src/api/persistence'

const canvasID = '11111111-1111-4111-8111-111111111111'
const sourceID = '22222222-2222-4222-8222-222222222222'
const targetID = '33333333-3333-4333-8333-333333333333'
const otherID = '44444444-4444-4444-8444-444444444444'
const parameters = (page: Page) => page.getByRole('button', { name: /^视频参数，/ })
const panel = (page: Page) => page.getByRole('dialog', { name: '视频提示词', exact: true })
const editor = (page: Page) => page.getByRole('textbox', { name: '视频提示词', exact: true })
async function setup(page: Page, reference?: 'image' | 'audio') {
  const canvas: SavedCanvas = { id: canvasID, title: '视频参数测试', version: 1, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString(), snapshot: {
    viewport: { x: 0, y: 0, zoom: 1 }, nodes: [
      { id: targetID, position: { x: 450, y: 140 }, data: { kind: 'video', name: '视频 A', prompt: '保留当前提示词', referenceIds: reference ? [sourceID] : [] } },
      { id: otherID, position: { x: 970, y: 140 }, data: { kind: 'video', name: '视频 B', prompt: '另一个视频的草稿' } },
    ],
  } }
  if (reference) {
    canvas.snapshot.nodes.unshift({ id: sourceID, position: { x: 20, y: 140 }, data: { kind: reference, name: '参考素材', origin: 'upload' } })
    const fixture = reference === 'image' ? 'sample-image.png' : 'reference-audio.wav'
    const bytes = readFileSync(fileURLToPath(new URL(`./fixtures/${fixture}`, import.meta.url)))
    const contentType = reference === 'image' ? 'image/png' : 'audio/wav'
    canvas.assets = [{ key: 'test-reference', url: '/api/assets/content?key=test-reference', canvasId: canvasID, nodeId: sourceID, kind: reference === 'image' ? 'images' : 'audios', source: 'uploads', contentType, bytes: bytes.length, media: reference === 'image' ? { width: 960, height: 540 } : { durationSeconds: 2 }, videoReference: { eligible: true } }]
    await page.route('**/api/assets/content?*', (route) => route.fulfill({ body: bytes, contentType }))
  }
  let generations = 0
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/canvases**', (route) => {
    if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      expect(body.version).toBe(canvas.version)
      canvas.snapshot = body.snapshot; canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.route('**/api/videos/generations', (route) => { generations++; return route.abort() })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  if (reference) await expect(page.getByRole('article', { name: '参考素材', exact: true })).not.toContainText('读取中')
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  return { canvas, calls: () => generations }
}

test('旧画布默认为 2.5 全能参考，模型菜单和文生视频模式可选择', async ({ page }, info) => {
  const db = await setup(page)
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  const models = page.getByRole('listbox', { name: '视频模型' })
  await expect(models.getByRole('option')).toHaveCount(4)
  await expect(models.getByRole('option').first()).toContainText('Seedance 2.5')
  await expect(models.getByRole('option').first()).toHaveAttribute('aria-selected', 'true')
  await page.screenshot({ path: info.outputPath('video-model-menu.png') })
  await page.keyboard.press('Escape')
  await expect(panel(page)).toBeVisible()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  const modes = page.getByRole('listbox', { name: '视频生成模式' })
  await expect(modes.getByRole('option')).toHaveCount(2)
  await expect(modes.getByRole('option', { name: /全能参考/ })).toBeEnabled()
  await expect(modes.getByRole('option', { name: /文生视频/ })).toBeEnabled()
  await expect(modes).toContainText('全能参考需至少添加 1 项参考素材')
  await page.screenshot({ path: info.outputPath('video-mode-unavailable.png') })
  await page.keyboard.press('Escape')
  await expect(editor(page)).toHaveText('保留当前提示词')
  expect(db.calls()).toBe(0)
})

test('参数弹层更改比例、画质与时长，滑块和数字同步，按节点保存并刷新恢复', async ({ page }, info) => {
  const db = await setup(page)
  await parameters(page).click()
  await page.getByRole('radio', { name: '9:16', exact: true }).check()
  await page.getByRole('radio', { name: '720P', exact: true }).check()
  await page.getByRole('slider', { name: '视频时长', exact: true }).fill('12')
  await expect(page.getByRole('spinbutton', { name: '视频时长秒数' })).toHaveValue('12')
  await page.getByRole('spinbutton', { name: '视频时长秒数' }).fill('15')
  await expect(page.getByRole('slider', { name: '视频时长', exact: true })).toHaveValue('15')
  await page.screenshot({ path: info.outputPath('video-parameters.png') })
  await page.keyboard.press('Escape')
  await expect(parameters(page)).toContainText('9:16 · 720P · 15s')
  await expect(editor(page)).toHaveText('保留当前提示词')
  await expect.poll(() => db.canvas.snapshot.nodes.find((n) => n.id === targetID)?.data.videoOptions?.duration).toBe(15)
  await page.getByRole('article', { name: '视频 B', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(parameters(page)).toContainText('16:9 · 480P · 4s')
  await expect(editor(page)).toHaveText('另一个视频的草稿')
  await page.reload()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(parameters(page)).toContainText('9:16 · 720P · 15s')
  expect(db.calls()).toBe(0)
})

test('切换到 2.0 自动裁剪时长，保存所选模型并重新获取该模型报价', async ({ page }) => {
  const db = await setup(page, 'image')
  const quotes: Record<string, unknown>[] = []
  await page.route('**/api/credits/quote', route => {
    const body = route.request().postDataJSON()
    quotes.push(body)
    return route.fulfill({ json: { points: body.model === 'doubao-seedance-2-0-260128' ? 44 : 82, priceVersion: 'test-v2' } })
  })
  await parameters(page).click()
  await page.getByRole('spinbutton', { name: '视频时长秒数' }).fill('30')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  await page.getByRole('listbox', { name: '视频模型' }).getByRole('option').nth(1).click()
  await expect(page.getByRole('button', { name: /^选择视频模型/ })).toContainText('Seedance 2.0')
  await expect(parameters(page)).toContainText('15s')
  await expect(panel(page)).toContainText('图片 1/9 · 视频 0/3 · 音频 0/3')
  await expect(page.locator('.credit-cost-trigger')).toHaveText('44')
  expect(quotes.at(-1)).toMatchObject({ model: 'doubao-seedance-2-0-260128', mode: 'reference', duration: 15 })
  await expect.poll(() => db.canvas.snapshot.nodes.find(node => node.id === targetID)?.data.videoModel).toBe('doubao-seedance-2-0-260128')
  await page.reload()
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('button', { name: /^选择视频模型/ })).toContainText('Seedance 2.0')
  await expect(parameters(page)).toContainText('15s')
  expect(db.calls()).toBe(0)
})

test('文生视频只发送提示词；已连接素材保留为草稿但不参与本次生成', async ({ page }, info) => {
  const db = await setup(page, 'image')
  const quotes: Record<string, unknown>[] = []
  const submissions: Record<string, unknown>[] = []
  await page.route('**/api/credits/quote', route => {
    const body = route.request().postDataJSON()
    quotes.push(body)
    return route.fulfill({ json: { points: body.mode === 'text' ? 64 : 82, priceVersion: 'test-v2' } })
  })
  page.on('request', request => {
    if (request.url().endsWith('/api/videos/generations')) submissions.push(request.postDataJSON())
  })
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await page.getByRole('option', { name: /文生视频/ }).click()
  await expect(panel(page)).toContainText('文生视频不会使用已连接参考素材')
  await expect(page.getByRole('button', { name: '参考', exact: true })).toBeDisabled()
  await expect(page.locator('.credit-cost-trigger')).toHaveText('64')
  await page.screenshot({ path: info.outputPath('text-to-video.png') })
  expect(quotes.at(-1)).toMatchObject({ mode: 'text', prompt: '保留当前提示词' })
  expect(quotes.at(-1)).not.toHaveProperty('references')
  await page.getByRole('button', { name: '生成视频', exact: true }).click()
  await expect.poll(() => submissions.length).toBe(1)
  expect(submissions[0]).toMatchObject({ mode: 'text', prompt: '保留当前提示词', acceptedPoints: 64 })
  expect(submissions[0]).not.toHaveProperty('references')
  expect(db.canvas.snapshot.nodes.find(node => node.id === targetID)?.data.referenceIds).toEqual([sourceID])
  expect(db.calls()).toBe(1)
})

test('文生视频空提示词与素材标签不能报价；2.0 家族拒绝纯音频参考', async ({ page }) => {
  const db = await setup(page, 'audio')
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  await page.getByRole('listbox', { name: '视频模型' }).getByRole('option').nth(3).click()
  await expect(panel(page)).toContainText('音频参考须搭配至少 1 项图片或视频')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await page.getByRole('option', { name: /文生视频/ }).click()
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  await editor(page).fill('')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  expect(db.calls()).toBe(0)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  db.canvas.snapshot.nodes.find(node => node.id === targetID)!.data.promptParts = [
    { type: 'text', text: '参考 ' }, { type: 'reference', nodeId: sourceID, kind: 'image', name: '参考素材' },
  ]
  db.canvas.snapshot.nodes.find(node => node.id === targetID)!.data.prompt = '参考 参考素材'
  await page.reload()
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(panel(page)).toContainText('文生视频不能包含素材标签')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
})

test('切换到较低素材上限时保留旧连线并明确阻止超限生成', async ({ page }) => {
  const db = await setup(page, 'image')
  const target = db.canvas.snapshot.nodes.find(node => node.id === targetID)!
  const ids = Array.from({ length: 9 }, (_, i) => `55555555-5555-4555-8555-${String(i).padStart(12, '0')}`)
  for (const [index, id] of ids.entries()) {
    db.canvas.snapshot.nodes.push({ id, position: { x: 1600 + index * 500, y: 140 }, data: { kind: 'image', name: `追加图片 ${index + 1}`, origin: 'upload' } })
    db.canvas.assets.push({ ...db.canvas.assets[0]!, key: `extra-${index}`, nodeId: id, url: `/api/assets/content?key=extra-${index}` })
  }
  target.data.referenceIds = [sourceID, ...ids]
  await page.reload()
  await expect(page.getByRole('article', { name: '追加图片 9', exact: true })).toContainText('960 × 540')
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: /^选择视频模型/ }).click()
  await page.getByRole('listbox', { name: '视频模型' }).getByRole('option').nth(1).click()
  await expect(panel(page)).toContainText('最多参考 9 项图片')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  expect(target.data.referenceIds).toEqual([sourceID, ...ids])
  await page.getByRole('button', { name: '移除参考 追加图片 9' }).click()
  await expect(panel(page)).toContainText('图片 9/9')
  await editor(page).fill('@')
  await page.locator('.mention-menu').getByRole('button', { name: '图片素材', exact: true }).click()
  const extra = page.locator('.mention-menu').getByRole('button', { name: '引用 追加图片 9', exact: true })
  await expect(extra).toHaveAttribute('aria-disabled', 'true')
  await expect(extra).toHaveAttribute('title', /最多参考 9 项图片/)
  await expect.poll(() => db.canvas.snapshot.nodes.find(node => node.id === targetID)?.data.referenceIds?.length).toBe(9)
  expect(db.calls()).toBe(0)
})

test('参考可用状态实时变化，2.5 音频单独引用仍可使用全能参考', async ({ page }) => {
  const db = await setup(page, 'audio')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await expect(page.getByRole('option', { name: /全能参考/ })).toBeEnabled()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '移除参考 参考素材' }).click()
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await expect(page.getByRole('listbox', { name: '视频生成模式' })).toContainText('全能参考需至少添加 1 项参考素材')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeDisabled()
  expect(db.calls()).toBe(0)
})

test('不合格参考不误报为可用；时长数字边界、原有声音开关与自动比例保持有效', async ({ page }) => {
  const db = await setup(page, 'image')
  await expect(page.getByRole('button', { name: '生成视频', exact: true })).toBeEnabled()
  db.canvas.assets[0]!.videoReference = { eligible: false, reason: '参考图片尺寸不符合要求' }
  await page.reload()
  await page.getByRole('article', { name: '视频 A', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('button', { name: /^选择视频生成类型/ }).click()
  await expect(page.getByRole('option', { name: /全能参考/ })).toBeEnabled()
  await expect(page.getByRole('listbox', { name: '视频生成模式' })).toContainText('参考图片尺寸不符合要求')
  await parameters(page).click()
  await expect(page.getByRole('listbox')).not.toBeVisible()
  const seconds = page.getByRole('spinbutton', { name: '视频时长秒数' })
  const duration = page.getByRole('slider', { name: '视频时长', exact: true })
  await seconds.fill('31'); await seconds.press('Tab')
  await expect(duration).toHaveValue('30')
  await seconds.fill('1'); await seconds.press('Tab')
  await expect(duration).toHaveValue('4')
  await seconds.fill(''); await seconds.press('Tab')
  await expect(seconds).toHaveValue('4')
  await page.getByRole('radio', { name: '自动比例' }).check()
  await page.getByLabel('生成声音', { exact: true }).uncheck()
  await page.keyboard.press('Escape')
  await expect(parameters(page)).toContainText('Auto · 480P · 4s')
  await expect.poll(() => db.canvas.snapshot.nodes.find((n) => n.id === targetID)?.data.videoOptions?.generateAudio).toBe(false)
  expect(db.calls()).toBe(0)
})

test('窄屏与放大后弹层不越界，键盘可操作，滚轮不缩放画布，Esc 逐层关闭', async ({ page }, info) => {
  await setup(page)
  await page.getByRole('button', { name: '放大输入框' }).click()
  await parameters(page).focus()
  await page.keyboard.press('ArrowUp')
  await expect(page.getByRole('dialog', { name: '视频参数', exact: true })).toBeVisible()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('radio', { name: '4:3', exact: true })).toBeChecked()
  for (const width of [390, 320, 1440]) {
    await page.setViewportSize({ width, height: 720 })
    await expect.poll(async () => {
      const box = await page.getByRole('dialog', { name: '视频参数', exact: true }).boundingBox()
      return box && box.x >= 15 && box.y >= 15 && box.x + box.width <= width - 15 && box.y + box.height <= 705
    }).toBe(true)
    const box = await page.getByRole('dialog', { name: '视频参数', exact: true }).boundingBox()
    await page.mouse.move(box!.x + 10, box!.y + 10)
    await page.mouse.wheel(0, -200)
    await expect(page.getByLabel('当前缩放比例')).toHaveText('100%')
    if (width === 390) await page.screenshot({ path: info.outputPath('video-parameters-mobile.png') })
  }
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '视频参数', exact: true })).not.toBeVisible()
  await expect(parameters(page)).toBeFocused()
  await expect(panel(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(panel(page)).not.toBeVisible()
})
