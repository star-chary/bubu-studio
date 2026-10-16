import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Locator, Page } from '@playwright/test'
import { expect, test } from './setup'
import type { CanvasSnapshot, SavedCanvas } from '../src/api/persistence'
import type { MediaKind } from '../src/canvas/media'

test.use({ viewport: { width: 1680, height: 1100 } })

const canvasId = '33333333-3333-4333-8333-333333333333'
const node = (page: Page, name: string) => page.getByRole('article', { name, exact: true })
const preview = (target: Locator) => target.locator('.media-node-preview')
const guide = (page: Page, axis: 'x' | 'y') => page.locator(`.node-alignment-guides line[data-axis="${axis}"]`)
interface Spec { name: string; x: number; y: number; width?: number; height?: number; kind?: MediaKind; generated?: boolean; empty?: boolean }

async function openCanvas(page: Page, specs: Spec[], viewport = { x: 0, y: 0, zoom: 1 }) {
  const saved: SavedCanvas = { id: canvasId, title: '节点对齐专项', version: 1, updatedAt: '2026-10-16T00:00:00Z',
    assets: [], tasks: [], results: [], snapshot: { viewport, nodes: [] } }
  const media = new Map<string, { contentType: string; body: Buffer }>()
  specs.forEach((spec, i) => {
    const id = `11111111-1111-4111-8111-${String(i + 1).padStart(12, '0')}`
    const kind = spec.kind ?? 'image'
    const width = spec.width ?? 240, height = spec.height ?? 300
    const file = kind === 'video' ? 'sample-video.mp4' : 'reference-audio.wav'
    const body = kind === 'image'
      ? Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><rect width="100%" height="100%" fill="${i ? '#405c76' : '#4c6860'}"/><circle cx="50%" cy="50%" r="45" fill="#ffffff30"/></svg>`)
      : readFileSync(fileURLToPath(new URL(`./fixtures/${file}`, import.meta.url)))
    const contentType = kind === 'image' ? 'image/svg+xml' : kind === 'video' ? 'video/mp4' : 'audio/wav'
    const key = `alignment-${id}`
    media.set(key, { body, contentType })
    if (!spec.empty) saved.assets.push({ key, url: `/api/assets/content?key=${key}`, canvasId, nodeId: id, bytes: body.length, contentType,
      kind: kind === 'image' ? 'images' : kind === 'video' ? 'videos' : 'audios', source: spec.generated ? 'generated' : 'uploads' })
    saved.snapshot.nodes.push({ id, position: { x: (spec.x - viewport.x) / viewport.zoom, y: (spec.y - viewport.y) / viewport.zoom },
      data: { kind, name: spec.name, origin: spec.generated ? 'generated' : 'upload', prompt: '保留节点草稿' } })
  })
  const writes: CanvasSnapshot[] = []
  const unexpected: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    if (!['GET', 'HEAD'].includes(request.method()) && !['PUT /api/canvases/' + canvasId, 'POST /api/credits/quote'].includes(`${request.method()} ${new URL(request.url()).pathname}`)) unexpected.push(request.url())
  })
  await page.route('**/api/persistence/config', (route) => route.fulfill({ json: { enabled: true } }))
  await page.route('**/api/assets/content?*', (route) => route.fulfill(media.get(new URL(route.request().url()).searchParams.get('key')!)!))
  await page.route('**/api/canvases**', (route) => {
    if (new URL(route.request().url()).pathname.endsWith('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
    if (route.request().method() === 'PUT') {
      saved.snapshot = route.request().postDataJSON().snapshot
      writes.push(saved.snapshot)
      return route.fulfill({ json: { version: ++saved.version } })
    }
    return route.fulfill({ json: saved })
  })
  await page.goto(`/?canvas=${canvasId}`)
  for (const spec of specs) {
    await expect(preview(node(page, spec.name))).toHaveAttribute('aria-busy', 'false')
    await expect.poll(async () => Math.round((await node(page, spec.name).boundingBox())!.x)).toBe(spec.x)
  }
  return { saved, writes, unexpected, errors }
}

async function beginDrag(page: Page, target: Locator) {
  const box = (await target.boundingBox())!
  const start = { x: box.x + 60, y: box.y + 12 }
  await page.mouse.move(start.x, start.y)
  await page.mouse.down()
  return async (x: number, y: number, steps = 1) => page.mouse.move(start.x + x - box.x, start.y + y - box.y, { steps })
}

async function expectGuide(page: Page, axis: 'x' | 'y', coordinate: number) {
  await expect(guide(page, axis)).toHaveCount(1)
  expect(Number(await guide(page, axis).getAttribute(`${axis}1`))).toBeCloseTo(coordinate, 1)
  expect(Number(await guide(page, axis).getAttribute(`${axis}2`))).toBeCloseTo(coordinate, 1)
}

for (const [anchor, horizontal, vertical] of [[0, '左边', '顶边'], [0.5, '水平中心', '垂直中心'], [1, '右边', '底边']] as const) {
  test(`不同尺寸展示框按${horizontal}吸附，参考线准确，松手保留位置`, async ({ page }) => {
    await openCanvas(page, [{ name: '基准', x: 400, y: 160 }, { name: '拖动', x: 1000, y: 650, width: 720, height: 300 }])
    const a = (await preview(node(page, '基准')).boundingBox())!
    const b = (await preview(node(page, '拖动')).boundingBox())!
    const expected = a.x + a.width * anchor - b.width * anchor
    const move = await beginDrag(page, node(page, '拖动'))
    await move(expected + 4, 650)
    await expect.poll(async () => (await preview(node(page, '拖动')).boundingBox())!.x).toBeCloseTo(expected, 1)
    await expectGuide(page, 'x', a.x + a.width * anchor)
    await expect(guide(page, 'y')).toHaveCount(0)
    await page.mouse.up()
    await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
    expect((await preview(node(page, '拖动')).boundingBox())!.x).toBeCloseTo(expected, 1)
  })

  test(`不同尺寸展示框按${vertical}吸附，保持另一轴自由拖动`, async ({ page }) => {
    await openCanvas(page, [{ name: '基准', x: 400, y: 160 }, { name: '拖动', x: 1000, y: 650, width: 720, height: 300 }])
    const a = (await preview(node(page, '基准')).boundingBox())!
    const b = (await preview(node(page, '拖动')).boundingBox())!
    const expected = a.y + a.height * anchor - b.height * anchor
    const move = await beginDrag(page, node(page, '拖动'))
    await move(1017, expected - 28 + 4)
    await expect.poll(async () => (await preview(node(page, '拖动')).boundingBox())!.y).toBeCloseTo(expected, 1)
    expect((await preview(node(page, '拖动')).boundingBox())!.x).toBeCloseTo(1017, 1)
    await expectGuide(page, 'y', a.y + a.height * anchor)
    await expect(guide(page, 'x')).toHaveCount(0)
    await page.mouse.up()
    await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
  })
}

for (const zoom of [0.5, 1, 2]) {
  test(`${zoom * 100}% 缩放和平移后按屏幕距离吸附和脱离，保存刷新仍对齐`, async ({ page }) => {
    const db = await openCanvas(page, [{ name: '基准', x: 150, y: 180 }, { name: '拖动', x: 950, y: 260 }], { x: -145, y: 90, zoom })
    const a = (await preview(node(page, '基准')).boundingBox())!
    const move = await beginDrag(page, node(page, '拖动'))
    for (const [distance, snapped] of [[7, false], [5, true], [9, true], [12, false], [5, true], [-9, true], [-12, false], [-5, true]] as const) {
      await move(950, 180 + distance)
      await expect.poll(async () => (await preview(node(page, '拖动')).boundingBox())!.y).toBeCloseTo(a.y + (snapped ? 0 : distance), 1)
      if (snapped) await expectGuide(page, 'y', a.y)
      else await expect(guide(page, 'y')).toHaveCount(0)
    }
    await page.mouse.up()
    await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
    await expect.poll(() => db.saved.snapshot.nodes[1]!.position.y).toBeCloseTo((180 - 90) / zoom, 4)
    // 吸附状态和参考线均不进入现有持久化协议。
    expect(Object.keys(db.saved.snapshot).sort()).toEqual(['nodes', 'viewport'])
    expect(Object.keys(db.saved.snapshot.nodes[1]!).sort()).toEqual(['data', 'id', 'position'])
    await page.reload()
    await expect(preview(node(page, '拖动'))).toHaveAttribute('aria-busy', 'false')
    await expect.poll(async () => (await preview(node(page, '拖动')).boundingBox())!.y).toBeCloseTo(a.y, 1)
    await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
    expect(db.unexpected).toEqual([])
    expect(db.errors).toEqual([])
  })
}

test('展示框边缘可以对齐另一框中心，不包含标题、工具框或提示词面板', async ({ page }, info) => {
  await openCanvas(page, [{ name: '基准', x: 400, y: 160, generated: true }, { name: '拖动', x: 1000, y: 650, width: 720, height: 300 }])
  await node(page, '基准').click({ position: { x: 80, y: 80 } })
  await expect(page.getByRole('dialog', { name: '图片提示词' })).toBeVisible()
  const a = (await preview(node(page, '基准')).boundingBox())!
  const move = await beginDrag(page, node(page, '拖动'))
  await expect(page.getByRole('dialog', { name: '图片提示词' })).toHaveCount(0)
  await expect(page.getByRole('toolbar', { name: '基准的素材工具' })).toBeVisible()
  await move(a.x + a.width / 2 + 4, a.y + a.height - 28 + 4)
  const b = (await preview(node(page, '拖动')).boundingBox())!
  expect(b.x).toBeCloseTo(a.x + a.width / 2, 1)
  expect(b.y).toBeCloseTo(a.y + a.height, 1)
  await expectGuide(page, 'x', b.x)
  await expectGuide(page, 'y', b.y)
  await page.screenshot({ path: info.outputPath('preview-alignment.png') })
  await page.mouse.up()

  // 将展示框顶边移到目标标题顶边附近，不应拿标题 / 整个节点边界吸附。
  const again = await beginDrag(page, node(page, '拖动'))
  await again(1000, 160 - 28 + 4)
  await expect(guide(page, 'y')).toHaveCount(0)
  expect((await preview(node(page, '拖动')).boundingBox())!.y).toBeCloseTo(164, 1)
  await page.mouse.up()
})

test('多候选选择最近的线，临界点不跳线，目标删除后清理参考线', async ({ page }) => {
  await openCanvas(page, [{ name: '基准一', x: 200, y: 180 }, { name: '基准二', x: 600, y: 188 }, { name: '拖动', x: 1100, y: 650 }])
  const move = await beginDrag(page, node(page, '拖动'))
  await move(1100, 185)
  await expectGuide(page, 'y', 188 + 28)
  await move(1100, 181)
  await expectGuide(page, 'y', 188 + 28)
  await move(1100, 177)
  await expectGuide(page, 'y', 180 + 28)
  await page.mouse.up()
  await node(page, '基准一').click({ button: 'right', position: { x: 60, y: 60 } })
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  const next = await beginDrag(page, node(page, '拖动'))
  await next(1100, 181)
  await expect(guide(page, 'y')).toHaveCount(0)
  await next(1100, 185)
  await expectGuide(page, 'y', 216)
  // 拖动中按 Delete 移除自身，不遗留线条或拖动状态。
  await page.locator('.canvas-page').focus()
  await page.keyboard.press('Delete')
  await expect(node(page, '拖动')).toHaveCount(0)
  await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
  await page.mouse.up()
})

test('自动平移时参考线继续贴合，失焦与松手后清理', async ({ page }) => {
  await openCanvas(page, [{ name: '基准', x: 200, y: 180 }, { name: '拖动', x: 900, y: 650 }])
  const pane = page.locator('.vue-flow__transformationpane')
  const before = await pane.getAttribute('style')
  const move = await beginDrag(page, node(page, '拖动'))
  await move(1615, 184)
  await expect.poll(() => pane.getAttribute('style')).not.toBe(before)
  await expectGuide(page, 'y', (await preview(node(page, '基准')).boundingBox())!.y)
  await page.evaluate(() => window.dispatchEvent(new Event('blur')))
  await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
  await page.mouse.up()
})

test('视频和音频展示框也可吸附，播放控件不触发拖动或参考线', async ({ page }) => {
  const db = await openCanvas(page, [{ name: '基准', x: 180, y: 170 }, { name: '视频', kind: 'video', x: 650, y: 210 }, { name: '音频', kind: 'audio', x: 1150, y: 650 }])
  for (const name of ['视频', '音频']) {
    const target = node(page, name)
    const box = (await target.boundingBox())!
    const move = await beginDrag(page, target)
    await move(box.x, 174)
    await expect.poll(async () => (await preview(target).boundingBox())!.y).toBeCloseTo(198, 1)
    await expectGuide(page, 'y', 198)
    await page.mouse.up()
    const before = await target.boundingBox()
    await target.getByRole('button', { name: `播放${name}`, exact: true }).click()
    await expect.poll(() => target.locator(name === '视频' ? 'video' : 'audio').evaluate((el: HTMLMediaElement) => el.paused)).toBe(false)
    await target.getByRole('button', { name: `暂停${name}`, exact: true }).click()
    expect(await target.boundingBox()).toEqual(before)
    await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
  }
  expect(db.unexpected).toEqual([])
  expect(db.errors).toEqual([])
})

test('空节点按占位展示框吸附，参考选择期间保持禁止拖动', async ({ page }) => {
  const db = await openCanvas(page, [{ name: '基准', x: 180, y: 170 }, { name: '空图片', x: 950, y: 230, generated: true, empty: true }])
  const target = node(page, '空图片')
  await expect(target.getByLabel('空图片节点')).toBeVisible()
  const move = await beginDrag(page, target)
  await move(950, 174)
  await expect.poll(async () => (await preview(target).boundingBox())!.y).toBeCloseTo(198, 1)
  await page.mouse.up()
  await target.click({ position: { x: 80, y: 80 } })
  await page.getByRole('dialog', { name: '图片提示词' }).getByRole('button', { name: '参考', exact: true }).click()
  await expect(page.getByRole('region', { name: '选择参考', exact: true })).toBeVisible()
  const before = (await target.boundingBox())!
  const baseBefore = (await node(page, '基准').boundingBox())!
  const selectionDrag = await beginDrag(page, target)
  await selectionDrag(850, 250)
  await page.mouse.up()
  // 参考选择期间允许平移整个画布，验证节点间相对位置及保存坐标，而非固定屏幕坐标。
  const after = (await target.boundingBox())!
  const baseAfter = (await node(page, '基准').boundingBox())!
  expect(after.x - baseAfter.x).toBeCloseTo(before.x - baseBefore.x, 1)
  expect(after.y - baseAfter.y).toBeCloseTo(before.y - baseBefore.y, 1)
  await expect.poll(() => db.saved.snapshot.nodes[1]!.position).toEqual({ x: 950, y: 170 })
  await expect(page.locator('.node-alignment-guides')).toHaveCount(0)
  await node(page, '基准').click({ position: { x: 80, y: 80 } })
  await expect(page.locator('.vue-flow__edge')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('region', { name: '选择参考', exact: true })).toHaveCount(0)
})
