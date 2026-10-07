import type { Page } from '@playwright/test'
import { expect, test } from './setup'

const lite = 'Doubao-Seedream-5.0-lite'
const pro = 'Doubao-Seedream-5.0-pro'
const selector = (page: Page) => page.getByRole('button', { name: /^选择模型/ })

async function addImage(page: Page, x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
}

test('模型列表只有 Lite 和 Pro，选择按节点保存，点击空白和 Esc 分层关闭', async ({ page }, testInfo) => {
  let calls = 0
  await page.route('**/api/images/generations', (route) => { calls++; return route.abort() })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addImage(page, 100, 150)
  await addImage(page, 820, 150)
  const first = page.getByRole('article', { name: '图片节点 1', exact: true })
  const second = page.getByRole('article', { name: '图片节点 2', exact: true })
  await first.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText(lite)
  await selector(page).click()
  const menu = page.getByRole('listbox', { name: '图片模型' })
  await expect(menu.getByRole('option')).toHaveText([/Doubao-Seedream-5.0-lite/, /Doubao-Seedream-5.0-pro/])
  await expect(menu.getByRole('option', { name: lite, exact: true })).toHaveAttribute('aria-selected', 'true')
  expect((await menu.boundingBox())!.y + (await menu.boundingBox())!.height).toBeLessThan((await selector(page).boundingBox())!.y)
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
  await second.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText(lite)
  await selector(page).click()
  // 切换节点时不会把旧节点展开的列表带过去。
  await first.click({ position: { x: 400, y: 70 } })
  await expect(menu).toHaveCount(0)
  await expect(selector(page)).toContainText(pro)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await first.click({ position: { x: 70, y: 80 } })
  await expect(selector(page)).toContainText(pro)
  expect(calls).toBe(0)
})

test('模型选择支持键盘，窄屏、放大及窗口调整后列表保持在窗口内且不缩放画布', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await addImage(page, 340, 620)
  await page.mouse.click(350, 670)
  const panel = page.getByRole('dialog')
  await selector(page).focus()
  await page.keyboard.press('ArrowDown')
  const menu = page.getByRole('listbox', { name: '图片模型' })
  await expect(page.getByRole('option', { name: lite, exact: true })).toBeFocused()
  await page.keyboard.press('End')
  await expect(page.getByRole('option', { name: pro, exact: true })).toBeFocused()
  await page.keyboard.press('Home')
  await expect(page.getByRole('option', { name: lite, exact: true })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await expect(selector(page)).toContainText(pro)
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
  await page.keyboard.press('Tab')
  await expect(menu).toHaveCount(0)
  await expect(panel).toBeVisible()
})
