import type { Page } from '@playwright/test'
import { expect, test, addNodeViaContextMenu } from './setup'

const lite = 'Doubao-Seedream-5.0-lite'
const pro = 'Doubao-Seedream-5.0-pro'
const gptImage = 'FLUX 3 Image（APIMart）'
const seedreamPro = 'Z Image Turbo（APIMart）'
const selector = (page: Page) => page.getByRole('button', { name: /^选择模型/ })

test('模型列表选择按节点保存，点击空白和 Esc 分层关闭', async ({ page }, testInfo) => {
  let calls = 0
  await page.route('**/api/images/generations', (route) => { calls++; return route.abort() })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addNodeViaContextMenu(page, 100, 150, '图片')
  await addNodeViaContextMenu(page, 820, 150, '图片')
  const first = page.getByRole('article', { name: '图片节点 1', exact: true })
  const second = page.getByRole('article', { name: '图片节点 2', exact: true })
  // 新节点默认未选择模型，不回退到 Ark 默认。
  await first.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText('选择模型')
  await selector(page).click()
  const menu = page.getByRole('listbox', { name: '图片模型' })
  await expect(menu.getByRole('option')).toHaveCount(8)
  await expect(menu.getByRole('option', { name: lite, exact: true })).toBeVisible()
  await expect(menu.getByRole('option', { name: gptImage, exact: true })).toBeVisible()
  await expect(menu.getByRole('option', { name: seedreamPro, exact: true })).toBeVisible()
  await expect(menu.getByRole('option', { name: gptImage, exact: true })).toHaveText('FLUX 3 Image')
  expect((await menu.boundingBox())!.y).toBeGreaterThanOrEqual(16)
  await menu.getByRole('option', { name: pro, exact: true }).click()
  await expect(menu).toHaveCount(0)
  await expect(selector(page)).toContainText(pro)
  await selector(page).click()
  await expect(menu.getByRole('option', { name: pro, exact: true })).toHaveAttribute('aria-selected', 'true')
  await page.screenshot({ path: testInfo.outputPath('image-model-menu.png') })
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(selector(page)).toBeFocused()
  await expect(page.getByRole('dialog')).toBeVisible()
  await selector(page).click()
  await page.getByRole('textbox').click({ position: { x: 500, y: 20 } })
  await expect(menu).toHaveCount(0)
  await expect(page.getByRole('dialog')).toBeVisible()
  // 选择按节点保存；新节点仍是未选择状态。
  await second.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText('选择模型')
  await selector(page).click()
  await first.click({ position: { x: 400, y: 70 } })
  await expect(menu).toHaveCount(0)
  await expect(selector(page)).toContainText(pro)
  // Esc 关闭面板需要焦点在面板内；先回到输入框再按 Esc。
  await page.getByRole('textbox').click({ position: { x: 500, y: 20 } })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await first.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText(pro)
  expect(calls).toBe(0)
})

test('模型选择支持键盘，窄屏、放大及窗口调整后列表保持在窗口内且不缩放画布', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addNodeViaContextMenu(page, 60, 120, '图片')
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 30 } })
  const panel = page.getByRole('dialog')
  await selector(page).focus()
  await page.keyboard.press('ArrowDown')
  const menu = page.getByRole('listbox', { name: '图片模型' })
  await expect(page.getByRole('option', { name: lite, exact: true })).toBeFocused()
  await page.keyboard.press('End')
  await expect(page.getByRole('option', { name: seedreamPro, exact: true })).toBeFocused()
  await page.keyboard.press('Home')
  await expect(page.getByRole('option', { name: lite, exact: true })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await expect(selector(page)).toContainText('Z Image Turbo')
  await expect(menu).toHaveCount(0)
  await panel.getByRole('button', { name: '放大输入框', exact: true }).click()
  await selector(page).click()

  async function withinViewport(width: number, height: number) {
    await expect.poll(async () => {
      const box = await menu.boundingBox()
      return !!box && box.x >= 16 && box.y >= 16 && box.x + box.width <= width - 16 && box.y + box.height <= height - 16
    }).toBe(true)
  }
  await withinViewport(375, 812)
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')
  await menu.hover()
  await page.mouse.wheel(0, 200)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
  await page.setViewportSize({ width: 320, height: 480 })
  await withinViewport(320, 480)
  await expect.poll(async () => {
    const box = (await menu.boundingBox())!
    return box.y + box.height < (await selector(page).boundingBox())!.y
  }).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('image-model-menu-mobile.png') })
  await expect(menu.getByRole('option', { name: seedreamPro, exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(menu).toHaveCount(0)
  await expect(panel).toBeVisible()
})
