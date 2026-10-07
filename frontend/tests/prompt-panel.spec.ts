import type { Page } from '@playwright/test'
import { expect, test } from './setup'
import { fileURLToPath } from 'node:url'

async function addNode(page: Page, kind: '图片' | '视频', x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: kind, exact: true }).click()
}

test('点击两种空节点弹出输入框，放大恢复保留文字和选区，各节点草稿独立', async ({ page }, testInfo) => {
  const errors: string[] = []
  const requests: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    if (['POST', 'PUT', 'PATCH'].includes(request.method())) requests.push(request.url())
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addNode(page, '图片', 80, 150)
  await addNode(page, '视频', 790, 150)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const image = page.getByRole('article', { name: '图片节点 1', exact: true })
  const video = page.getByRole('article', { name: '视频节点 2', exact: true })
  await image.click({ position: { x: 80, y: 80 } })
  const panel = page.getByRole('dialog', { name: '图片提示词' })
  const input = panel.getByRole('textbox', { name: '图片提示词' })
  await expect(input).toBeFocused()
  await expect(panel.getByRole('button')).toHaveCount(4)
  const nodeBox = await image.boundingBox()
  const initialBox = await panel.boundingBox()
  expect(initialBox!.y).toBeGreaterThan(nodeBox!.y + nodeBox!.height)
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')
  const draft = '雨后的街道，暖色路灯倒映在积水中。\n保持安静的夜晚氛围。'
  await input.fill(draft)
  await input.press('Control+Home')
  await input.press('Shift+ArrowRight')
  const selection = await input.evaluate(() => { const selection = window.getSelection()!; return [selection.anchorOffset, selection.focusOffset, selection.toString()] })
  await page.screenshot({ path: testInfo.outputPath('prompt-panel-normal.png') })

  await panel.getByRole('button', { name: '放大输入框', exact: true }).click()
  await expect(panel.getByRole('button', { name: '恢复输入框大小' })).toBeVisible()
  const expandedBox = await panel.boundingBox()
  expect(expandedBox!.width).toBeGreaterThan(initialBox!.width)
  expect(expandedBox!.height).toBeGreaterThan(initialBox!.height)
  await expect(input).toHaveText(draft, { useInnerText: true })
  await expect(input).toBeFocused()
  expect(await input.evaluate(() => { const selection = window.getSelection()!; return [selection.anchorOffset, selection.focusOffset, selection.toString()] })).toEqual(selection)
  await page.screenshot({ path: testInfo.outputPath('prompt-panel-expanded.png') })
  await panel.getByRole('button', { name: '恢复输入框大小' }).click()
  expect(await panel.boundingBox()).toEqual(initialBox)
  await expect(input).toHaveText(draft, { useInnerText: true })
  expect(await image.boundingBox()).toEqual(nodeBox)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)

  await video.click({ position: { x: 80, y: 80 } })
  await expect(page.getByRole('dialog')).toHaveCount(1)
  const videoInput = page.getByRole('textbox', { name: '视频提示词' })
  await expect(videoInput).toHaveText('')
  await videoInput.fill('镜头缓慢向前移动。')
  await image.click({ position: { x: 80, y: 80 } })
  await expect(input).toHaveText(draft, { useInnerText: true })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await video.click({ position: { x: 80, y: 80 } })
  await expect(videoInput).toHaveText('镜头缓慢向前移动。')
  await page.mouse.click(50, 750)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(errors).toEqual([])
  expect(requests).toEqual([])
})

test('上传素材也能打开输入框，视频控件和节点拖动不误弹面板', async ({ page }) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await page.mouse.click(80, 140, { button: 'right' })
  const chooserPromise = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooserPromise).setFiles([
    fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url)),
    fileURLToPath(new URL('./fixtures/sample-video.mp4', import.meta.url)),
  ])
  const videoNode = page.getByRole('article', { name: 'sample-video.mp4', exact: true })
  const video = videoNode.locator('video')
  await videoNode.getByRole('button', { name: '播放视频' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await videoNode.getByRole('button', { name: '暂停视频' }).click()
  await videoNode.getByRole('slider', { name: '视频进度' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  await videoNode.click({ position: { x: 100, y: 100 } })
  const input = page.getByRole('textbox', { name: '视频提示词' })
  await expect(input).toBeFocused()
  await input.fill('测试视频草稿')
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  await page.keyboard.press('Escape')
  const before = (await videoNode.boundingBox())!
  await page.mouse.move(before.x + 80, before.y + 80)
  await page.mouse.down()
  await page.mouse.move(before.x + 130, before.y + 120, { steps: 8 })
  await page.mouse.up()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect((await videoNode.boundingBox())!.x - before.x).toBeCloseTo(50, 1)
  await videoNode.click({ position: { x: 100, y: 100 } })
  await expect(input).toHaveText('测试视频草稿')
  await page.getByRole('article', { name: 'sample-image.png', exact: true }).click({ position: { x: 100, y: 100 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toBeFocused()
  await page.mouse.click(50, 750, { button: 'right' })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('menu')).toBeVisible()
})

test('窗口边缘和窄屏下输入框不越界，放大后可滚动长文本且不缩放画布', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addNode(page, '图片', 340, 620)
  await page.mouse.click(350, 670)
  const panel = page.getByRole('dialog', { name: '图片提示词' })
  const input = panel.getByRole('textbox')
  await expect(input).toBeVisible()

  async function expectWithinWindow(width: number, height: number) {
    await expect.poll(async () => {
      const box = await panel.boundingBox()
      return !!box && box.x >= 16 && box.y >= 16 && box.x + box.width <= width - 16 && box.y + box.height <= height - 16
    }).toBe(true)
  }

  await expectWithinWindow(375, 812)
  await panel.getByRole('button', { name: '放大输入框', exact: true }).click()
  await expectWithinWindow(375, 812)
  const text = Array.from({ length: 50 }, (_, index) => `第 ${index + 1} 行：描述画面中的细节。`).join('\n')
  await input.fill(text)
  await input.press('Control+Home')
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')
  await input.hover()
  await page.mouse.wheel(0, 350)
  await expect.poll(() => input.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
  await page.screenshot({ path: testInfo.outputPath('prompt-panel-mobile.png') })
  await page.setViewportSize({ width: 320, height: 480 })
  await expectWithinWindow(320, 480)
  await panel.getByRole('button', { name: '恢复输入框大小' }).click()
  await expectWithinWindow(320, 480)
  await expect(input).toHaveText(text, { useInnerText: true })
})
