import type { Page } from '@playwright/test'
import { expect, test } from './setup'

// 从实际渲染的变换读取视口，验证用户看到的结果，而非调用组件内部方法。
async function readViewport(page: Page) {
  return page.locator('.vue-flow__transformationpane').evaluate((element) => {
    const transform = new DOMMatrixReadOnly(getComputedStyle(element).transform)
    return { x: transform.e, y: transform.f, zoom: transform.a }
  })
}

async function expectInitialViewport(page: Page) {
  await expect.poll(() => readViewport(page)).toEqual({ x: 0, y: 0, zoom: 1 })
  await expect(page.getByLabel('当前缩放比例')).toHaveText('100%')
}

test('直接进入临时画布，保持零节点、零连线且没有运行错误', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text())
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await expect(page.getByRole('heading', { name: '临时画布' })).toBeVisible()
  await expect(page.locator('.vue-flow__background')).toBeVisible()
  await expect(page.locator('.vue-flow__node')).toHaveCount(0)
  await expect(page.locator('.vue-flow__edge')).toHaveCount(0)
  await expectInitialViewport(page)
  await page.screenshot({ path: testInfo.outputPath('canvas-desktop.png') })
  expect(errors).toEqual([])
})

test('滚轮以鼠标为缩放中心，限制 25%–200%，工具栏和重置同步', async ({ page }) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await expectInitialViewport(page)
  const anchor = { x: 430, y: 310 }
  await page.mouse.move(anchor.x, anchor.y)
  await page.mouse.wheel(0, -240)
  await expect.poll(async () => (await readViewport(page)).zoom).toBeGreaterThan(1)
  const zoomed = await readViewport(page)
  expect((anchor.x - zoomed.x) / zoomed.zoom).toBeCloseTo(anchor.x, 2)
  expect((anchor.y - zoomed.y) / zoomed.zoom).toBeCloseTo(anchor.y, 2)
  await expect(page.getByLabel('当前缩放比例')).toHaveText(`${Math.round(zoomed.zoom * 100)}%`)

  await page.mouse.wheel(0, -20000)
  await expect(page.getByLabel('当前缩放比例')).toHaveText('200%')
  await expect(page.getByRole('button', { name: '放大画布', exact: true })).toBeDisabled()
  await page.mouse.wheel(0, 20000)
  await expect(page.getByLabel('当前缩放比例')).toHaveText('25%')
  await expect(page.getByRole('button', { name: '缩小画布', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '重置视图' }).click()
  await expectInitialViewport(page)

  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  await expect(page.getByLabel('当前缩放比例')).toHaveText('120%')
  await page.getByRole('button', { name: '缩小画布', exact: true }).click()
  await expectInitialViewport(page)

  // 滚动工具栏不会穿透到画布。
  await page.getByLabel('当前缩放比例').hover()
  await page.mouse.wheel(0, 300)
  await expectInitialViewport(page)
})

test('缩放后仍按屏幕距离平移，移出松手不粘连，刷新恢复初始状态', async ({ page }) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await expectInitialViewport(page)
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  const before = await readViewport(page)
  await page.mouse.move(420, 300)
  await page.mouse.down()
  await page.mouse.move(560, 390, { steps: 12 })
  await page.mouse.up()
  const after = await readViewport(page)
  expect(after.x - before.x).toBeCloseTo(140, 2)
  expect(after.y - before.y).toBeCloseTo(90, 2)
  expect(after.zoom).toBe(before.zoom)

  await page.mouse.move(500, 350)
  await page.mouse.down()
  await page.mouse.move(1500, 960, { steps: 10 })
  await page.mouse.up()
  const released = await readViewport(page)
  await page.mouse.move(600, 400, { steps: 10 })
  expect(await readViewport(page)).toEqual(released)
  await page.getByRole('button', { name: '重置视图' }).click()
  await expectInitialViewport(page)

  await page.mouse.move(400, 300)
  await page.mouse.down()
  await page.mouse.move(480, 360, { steps: 5 })
  await page.mouse.up()
  await page.reload()
  await expectInitialViewport(page)
})

test('窄屏和窗口调整没有溢出，按钮可通过键盘操作', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await expectInitialViewport(page)
  const canvas = await page.getByRole('main').boundingBox()
  expect(canvas).toMatchObject({ width: 375, height: 812 })
  const toolbar = await page.getByRole('group', { name: '画布视图控制' }).boundingBox()
  expect(toolbar!.x).toBeGreaterThanOrEqual(0)
  expect(toolbar!.x + toolbar!.width).toBeLessThanOrEqual(375)
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '返回首页', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '缩小画布', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect.poll(async () => (await readViewport(page)).zoom).toBeLessThan(1)
  await page.getByRole('button', { name: '重置视图' }).click()
  await expectInitialViewport(page)
  await page.screenshot({ path: testInfo.outputPath('canvas-mobile.png') })
  await page.setViewportSize({ width: 1024, height: 640 })
  await expect.poll(async () => {
    const box = await page.getByRole('main').boundingBox()
    return { width: box?.width, height: box?.height }
  }).toEqual({ width: 1024, height: 640 })
  await expectInitialViewport(page)
})

test('右键菜单仅包含指定入口，点击添加节点替换同一面板，外部和 Esc 可关闭', async ({ page }, testInfo) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await expectInitialViewport(page)
  await page.mouse.click(500, 250, { button: 'right' })
  const menu = page.getByRole('menu')
  await expect(menu.getByRole('menuitem')).toHaveText(['上传', '添加节点'])
  const initialBox = await menu.boundingBox()
  expect(initialBox).toMatchObject({ x: 500, y: 250 })
  await menu.screenshot({ path: testInfo.outputPath('context-menu-main.png') })

  await menu.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await expect(menu).toHaveCount(1)
  await expect(menu).toHaveAccessibleName('添加节点')
  await expect(menu.getByRole('menuitem')).toHaveText(['图片', '视频'])
  expect(await menu.boundingBox()).toMatchObject({ x: initialBox!.x, y: initialBox!.y })
  await menu.screenshot({ path: testInfo.outputPath('context-menu-nodes.png') })
  await expectInitialViewport(page)

  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await page.mouse.click(600, 300, { button: 'right' })
  await expect(menu.getByRole('menuitem')).toHaveText(['上传', '添加节点'])
  await page.mouse.click(300, 250)
  await expect(menu).toHaveCount(0)

  await page.mouse.click(500, 250, { button: 'right' })
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(menu.getByRole('menuitem')).toHaveText(['图片', '视频'])
  await menu.getByRole('menuitem', { name: '图片', exact: true }).click()
  await expect(menu).toHaveCount(0)
  await expect(page.locator('.vue-flow__node')).toHaveCount(1)
  await expect(page.getByRole('article', { name: '图片节点 1', exact: true })).toBeVisible()
})

test('菜单在窗口边缘和缩放后正确定位，切换内容与调整窗口不越界', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  const before = await readViewport(page)
  await page.mouse.click(370, 805, { button: 'right' })
  const menu = page.getByRole('menu')
  await expect(menu).toBeVisible()

  async function expectInViewport(width: number, height: number) {
    await expect.poll(async () => {
      const box = await menu.boundingBox()
      return !!box && box.x >= 8 && box.y >= 8
        && box.x + box.width <= width - 8 && box.y + box.height <= height - 8
    }).toBe(true)
  }

  await expectInViewport(375, 812)
  await menu.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await expect(menu.getByRole('menuitem')).toHaveText(['图片', '视频'])
  await expectInViewport(375, 812)
  await menu.getByRole('menuitem', { name: '视频', exact: true }).hover()
  await page.mouse.wheel(0, -400)
  expect(await readViewport(page)).toEqual(before)
  await page.setViewportSize({ width: 320, height: 480 })
  await expectInViewport(320, 480)
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)

  // 缩放之后依然在鼠标的屏幕坐标处打开，而不是画布坐标。
  await page.mouse.click(40, 180, { button: 'right' })
  await expect(menu.getByRole('menuitem')).toHaveText(['上传', '添加节点'])
  expect(await menu.boundingBox()).toMatchObject({ x: 40, y: 180 })
})
