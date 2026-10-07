import type { Locator, Page } from '@playwright/test'
import { expect, test } from './setup'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'

test.use({ viewport: { width: 1680, height: 1000 } })

const videoPath = fileURLToPath(new URL('./fixtures/sample-video.mp4', import.meta.url))
const videoFile = { name: 'sample-video.mp4', mimeType: 'video/mp4', buffer: readFileSync(videoPath) }
function imageFile(name: string, color = '#446778') {
  return { name, mimeType: 'image/svg+xml', buffer: Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="300"><rect width="240" height="300" fill="${color}"/><circle cx="120" cy="120" r="64" fill="#ffffff30"/></svg>`) }
}
const node = (page: Page, name: string) => page.getByRole('article', { name, exact: true })
const edges = (page: Page) => page.locator('.vue-flow__edge')
const panel = (page: Page) => page.getByRole('dialog')
const banner = (page: Page) => page.getByRole('region', { name: '选择参考', exact: true })

async function upload(page: Page, files: Parameters<import('@playwright/test').FileChooser['setFiles']>[0], x = 70, y = 130) {
  await page.mouse.click(x, y, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(files)
}

async function addNode(page: Page, kind: '图片' | '视频', x = 1120, y = 140) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
}

async function clickNode(target: Locator) {
  await target.click({ position: { x: 70, y: 80 } })
}

test('图片只能参考有效图片，拒绝自身/空节点/损坏素材，多选去重和移除同步连线', async ({ page }, testInfo) => {
  const errors: string[] = []
  const writes: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => { if (['POST', 'PUT', 'PATCH'].includes(request.method())) writes.push(request.url()) })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, [imageFile('参考一.svg'), videoFile, imageFile('参考二.svg', '#49664f'), { name: 'broken.png', mimeType: 'image/png', buffer: Buffer.from('broken') }])
  await expect(node(page, '参考二.svg')).toContainText('240 × 300')
  await expect(node(page, 'broken.png').getByRole('alert')).toBeVisible()
  await addNode(page, '图片')
  await addNode(page, '图片', 1120, 760)
  const target = node(page, '图片节点 1')
  await clickNode(target)
  await panel(page).getByRole('textbox').fill('保持目标节点草稿')
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await expect(banner(page)).toContainText('可选图片 · 已选 0 项')
  await clickNode(target)
  await expect(banner(page)).toContainText('不能参考节点自身')
  await clickNode(node(page, 'sample-video.mp4'))
  await expect(banner(page)).toContainText('图片节点只能参考图片')
  await clickNode(node(page, '图片节点 2'))
  await expect(banner(page)).toContainText('空节点或读取失败的素材不能作为参考')
  await expect(edges(page)).toHaveCount(0)
  await clickNode(node(page, 'broken.png'))
  await expect(banner(page)).toContainText('请选择已成功加载的素材')

  await clickNode(node(page, '参考一.svg'))
  await expect(edges(page)).toHaveCount(1)
  await expect(panel(page).getByRole('listitem')).toHaveCount(1)
  await clickNode(node(page, '参考一.svg'))
  await expect(banner(page)).toContainText('此素材已添加')
  await expect(edges(page)).toHaveCount(1)
  await clickNode(node(page, '参考二.svg'))
  await expect(edges(page)).toHaveCount(2)
  await expect(panel(page).getByRole('listitem')).toHaveCount(2)
  await expect(panel(page).getByRole('textbox')).toHaveText('保持目标节点草稿')
  await page.screenshot({ path: testInfo.outputPath('image-references.png') })

  await panel(page).getByRole('button', { name: '移除参考 参考一.svg', exact: true }).click()
  await expect(edges(page)).toHaveCount(1)
  await expect(panel(page).getByRole('listitem')).toHaveCount(1)
  await expect(panel(page).locator('.reference-index')).toHaveText('1')
  await page.keyboard.press('Escape')
  await expect(banner(page)).toHaveCount(0)
  await expect(panel(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(panel(page)).toHaveCount(0)
  await clickNode(target)
  await expect(panel(page).getByRole('img', { name: '参考二.svg' })).toBeVisible()
  expect(errors).toEqual([])
  expect(writes).toEqual([])
})

test('视频可混合引用图片和视频，播放控件不误选，同一素材可供不同节点独立引用', async ({ page }, testInfo) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, [imageFile('共用图片.svg'), videoFile])
  await expect(node(page, 'sample-video.mp4').getByRole('button', { name: '播放视频' })).toBeVisible()
  await addNode(page, '视频')
  await addNode(page, '图片', 1120, 700)
  const videoTarget = node(page, '视频节点 1')
  await clickNode(videoTarget)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await expect(banner(page)).toContainText('可选图片、视频和音频')
  const sourceVideo = node(page, 'sample-video.mp4')
  await sourceVideo.getByRole('button', { name: '播放视频' }).click()
  await expect(edges(page)).toHaveCount(0)
  await expect(banner(page)).toBeVisible()
  await sourceVideo.getByRole('button', { name: '暂停视频' }).click()
  await clickNode(sourceVideo)
  await expect(edges(page)).toHaveCount(1)
  expect(await sourceVideo.locator('video').evaluate((el: HTMLVideoElement) => el.paused)).toBe(true)
  await clickNode(node(page, '共用图片.svg'))
  await expect(panel(page).getByRole('listitem')).toHaveCount(2)
  await expect(panel(page).locator('video')).toHaveCount(1)
  await page.screenshot({ path: testInfo.outputPath('video-references.png') })
  await banner(page).getByRole('button', { name: '结束参考选择' }).click()

  await page.keyboard.press('Escape')
  await clickNode(node(page, '图片节点 2'))
  await expect(panel(page).getByRole('listitem')).toHaveCount(0)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await clickNode(node(page, '共用图片.svg'))
  await expect(edges(page)).toHaveCount(3)
  await panel(page).getByRole('button', { name: '移除参考 共用图片.svg' }).click()
  await expect(edges(page)).toHaveCount(2)
  await page.keyboard.press('Escape')
  await clickNode(videoTarget)
  await expect(panel(page).getByRole('listitem')).toHaveCount(2)
  await page.reload()
  await expect(edges(page)).toHaveCount(0)
  await expect(page.locator('.media-node')).toHaveCount(0)
})

test('连线端点跟随拖动、缩放和平移，返回节点保留引用并定位目标', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error' || message.type() === 'warning') errors.push(message.text()) })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, imageFile('位置参考.svg'))
  await addNode(page, '图片', 1020, 150)
  const source = node(page, '位置参考.svg')
  const target = node(page, '图片节点 1')
  await clickNode(target)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await clickNode(source)
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  const path = page.locator('.vue-flow__edge-path')

  // 读取实际 SVG 端点的屏幕位置，与节点实际端点比较，不能只断言有一条线。
  async function expectAttached() {
    await expect(path).toHaveCount(1)
    await expect.poll(async () => {
      const endpoints = await path.evaluate((element: SVGPathElement) => {
        const matrix = element.getScreenCTM()!
        const start = element.getPointAtLength(0).matrixTransform(matrix)
        const end = element.getPointAtLength(element.getTotalLength()).matrixTransform(matrix)
        return { start: { x: start.x, y: start.y }, end: { x: end.x, y: end.y } }
      })
      const from = (await source.locator('.source').boundingBox())!
      const to = (await target.locator('.target').boundingBox())!
      return Math.max(Math.abs(endpoints.start.x - (from.x + from.width)), Math.abs(endpoints.start.y - (from.y + from.height / 2)), Math.abs(endpoints.end.x - to.x), Math.abs(endpoints.end.y - (to.y + to.height / 2)))
    }).toBeLessThan(1)
  }
  await expectAttached()
  const before = (await source.boundingBox())!
  const oldPath = await path.getAttribute('d')
  await page.mouse.move(before.x + 70, before.y + 80)
  await page.mouse.down()
  await page.mouse.move(before.x + 160, before.y + 140, { steps: 8 })
  await page.mouse.up()
  await expect(path).not.toHaveAttribute('d', oldPath!)
  await expectAttached()
  await page.getByRole('button', { name: '缩小画布', exact: true }).click()
  await expectAttached()
  await page.mouse.move(40, 850)
  await page.mouse.down()
  await page.mouse.move(180, 790, { steps: 8 })
  await page.mouse.up()
  await expectAttached()

  await clickNode(target)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await page.mouse.move(40, 880)
  await page.mouse.down()
  await page.mouse.move(540, 880, { steps: 10 })
  await page.mouse.up()
  await expect(banner(page)).toBeVisible()
  await banner(page).getByRole('button', { name: '返回节点' }).click()
  await expect(banner(page)).toHaveCount(0)
  const centered = (await target.boundingBox())!
  expect(centered.x + centered.width / 2).toBeCloseTo(840, 1)
  expect(centered.y + centered.height / 2).toBeCloseTo(500, 1)
  await expect(panel(page).getByRole('listitem')).toHaveCount(1)
  await expectAttached()
  await page.screenshot({ path: testInfo.outputPath('reference-lines.png') })
  expect(errors).toEqual([])
})

test('放大面板进入参考时收起，窄屏提示不越界，Esc 退出与取消组词互不干扰', async ({ page }, testInfo) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, imageFile('窄屏参考.svg'))
  await addNode(page, '图片', 1020, 150)
  await clickNode(node(page, '图片节点 1'))
  await panel(page).getByRole('textbox').fill('保留输入')
  await panel(page).getByRole('button', { name: '放大输入框', exact: true }).click()
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await expect(panel(page)).not.toHaveClass(/is-expanded/)
  await clickNode(node(page, '窄屏参考.svg'))
  await page.setViewportSize({ width: 375, height: 812 })
  await expect.poll(async () => {
    const box = (await banner(page).boundingBox())!
    const editor = (await panel(page).boundingBox())!
    return box.x >= 16 && box.x + box.width <= 359 && editor.x >= 16 && editor.x + editor.width <= 359
  }).toBe(true)
  await panel(page).getByRole('textbox').dispatchEvent('keydown', { key: 'Escape', isComposing: true })
  await expect(banner(page)).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('references-mobile.png') })
  await page.keyboard.press('Escape')
  await expect(banner(page)).toHaveCount(0)
  await expect(panel(page).getByRole('textbox')).toHaveText('保留输入')
  await expect(panel(page).getByRole('listitem')).toHaveCount(1)
})

test('被引用素材后续读取失败，清理所有目标中的缩略图、引用端点和连线', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await upload(page, imageFile('失效参考.svg'))
  await addNode(page, '图片')
  await addNode(page, '视频', 1120, 700)
  const source = node(page, '失效参考.svg')
  for (const name of ['图片节点 1', '视频节点 2']) {
    await clickNode(node(page, name))
    await panel(page).getByRole('button', { name: '参考', exact: true }).click()
    await clickNode(source)
    await page.keyboard.press('Escape')
  }
  await expect(edges(page)).toHaveCount(2)
  // 故障注入：模拟浏览器在素材被选中之后报告读取失败。
  await source.locator('img').dispatchEvent('error', { bubbles: false })
  await expect(source.getByRole('alert')).toContainText('无法预览此文件')
  await expect(edges(page)).toHaveCount(0)
  await expect(panel(page).getByRole('listitem')).toHaveCount(0)
  await expect(page.locator('.reference-handle.is-visible')).toHaveCount(0)
  await clickNode(node(page, '图片节点 1'))
  await expect(panel(page).getByRole('listitem')).toHaveCount(0)
  await panel(page).getByRole('button', { name: '参考', exact: true }).click()
  await clickNode(source)
  await expect(banner(page)).toContainText('请选择已成功加载的素材')
  await expect(edges(page)).toHaveCount(0)
  expect(errors).toEqual([])
})
