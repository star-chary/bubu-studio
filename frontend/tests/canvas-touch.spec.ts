import type { Page } from '@playwright/test'
import type { SavedCanvas } from '../src/api/persistence'
import { addNodeViaContextMenu, expect, test } from './setup'

const canvasId = '33333333-3333-4333-8333-333333333333'

async function readViewport(page: Page) {
  return page.locator('.vue-flow__transformationpane').evaluate((element) => {
    const transform = new DOMMatrixReadOnly(getComputedStyle(element).transform)
    return { x: transform.e, y: transform.f, zoom: transform.a }
  })
}

async function swipe(page: Page, start: { x: number; y: number }, delta: { x: number; y: number }) {
  const client = await page.context().newCDPSession(page)
  try {
    await client.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [start] })
    for (let step = 1; step <= 10; step++) {
      await client.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x: start.x + delta.x * step / 10, y: start.y + delta.y * step / 10 }],
      })
    }
    await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  } finally {
    await client.detach()
  }
}

for (const viewport of [{ width: 390, height: 844 }, { width: 320, height: 568 }]) {
  test.describe(`手机画布 ${viewport.width}×${viewport.height}`, () => {
    test.use({ viewport, isMobile: true, hasTouch: true })

    test('单指在空白处拖动画布，松手后可以反向拖动', async ({ page }) => {
      await page.goto(`/?temporary=${canvasId}`)
      await expect(page.getByLabel('画布保存状态')).toHaveText('临时画布')
      await expect(page.getByText('长按空白处或节点打开菜单')).toBeVisible()
      const before = await readViewport(page)

      await swipe(page, { x: 100, y: 200 }, { x: 70, y: 80 })
      await expect.poll(async () => (await readViewport(page)).x - before.x).toBeCloseTo(70, 1)
      const moved = await readViewport(page)
      expect(moved.y - before.y).toBeCloseTo(80, 1)
      expect(moved.zoom).toBe(before.zoom)

      await swipe(page, { x: 170, y: 280 }, { x: -40, y: -50 })
      const returned = await readViewport(page)
      expect(returned.x - before.x).toBeCloseTo(30, 1)
      expect(returned.y - before.y).toBeCloseTo(30, 1)
    })
  })
}

test('电脑中键拖动画布，右键仍然打开菜单', async ({ page }) => {
  await page.goto(`/?temporary=${canvasId}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('临时画布')
  const before = await readViewport(page)
  await page.mouse.move(400, 200)
  await page.mouse.down({ button: 'middle' })
  await page.mouse.move(460, 250, { steps: 10 })
  await page.mouse.up({ button: 'middle' })
  const moved = await readViewport(page)
  expect(moved.x - before.x).toBeCloseTo(60, 1)
  expect(moved.y - before.y).toBeCloseTo(50, 1)

  await page.mouse.click(500, 200, { button: 'right' })
  await expect(page.getByRole('menu').getByRole('menuitem')).toHaveText(['上传', '添加节点'])
  expect(await readViewport(page)).toEqual(moved)
})

test.describe('手机画布交互', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  test('双指缩放和画布工具栏可用', async ({ page }) => {
    await page.goto(`/?temporary=${canvasId}`)
    await expect(page.getByLabel('画布保存状态')).toHaveText('临时画布')
    const client = await page.context().newCDPSession(page)
    try {
      await client.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 140, y: 300, id: 1 }, { x: 240, y: 300, id: 2 }] })
      for (let step = 1; step <= 10; step++) {
        await client.send('Input.dispatchTouchEvent', {
          type: 'touchMove',
          touchPoints: [{ x: 140 - step * 3, y: 300, id: 1 }, { x: 240 + step * 3, y: 300, id: 2 }],
        })
      }
      await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    } finally {
      await client.detach()
    }
    await expect.poll(async () => (await readViewport(page)).zoom).toBeGreaterThan(1.4)
    await page.getByRole('button', { name: '重置视图' }).tap()
    await expect.poll(() => readViewport(page)).toEqual({ x: 0, y: 0, zoom: 1 })
  })

  test('触摸拖动节点时只移动节点，轻触仍能打开编辑面板', async ({ page }) => {
    await page.goto(`/?temporary=${canvasId}`)
    await expect(page.getByLabel('画布保存状态')).toHaveText('临时画布')
    await addNodeViaContextMenu(page, 30, 180, '图片')
    const node = page.getByRole('article', { name: '图片节点 1', exact: true })
    const beforeNode = (await node.boundingBox())!
    const beforeViewport = await readViewport(page)

    await swipe(page, { x: beforeNode.x + 60, y: beforeNode.y + 70 }, { x: 30, y: 40 })
    await expect.poll(async () => (await node.boundingBox())!.x - beforeNode.x).toBeCloseTo(30, 1)
    const movedNode = (await node.boundingBox())!
    expect(movedNode.y - beforeNode.y).toBeCloseTo(40, 1)
    expect(await readViewport(page)).toEqual(beforeViewport)

    await page.touchscreen.tap(movedNode.x + 60, movedNode.y + 70)
    await expect(page.getByRole('textbox')).toBeVisible()
  })

  test('拖动结果通过现有画布快照保存并在刷新后恢复', async ({ page }) => {
    const canvas: SavedCanvas = { id: canvasId, title: '手机画布', version: 0, snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } }, assets: [], tasks: [], results: [], updatedAt: new Date().toISOString() }
    await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
    await page.route('**/api/canvases**', route => {
      if (route.request().url().includes('/tasks')) return route.fulfill({ json: { tasks: [], hasMore: false } })
      if (route.request().method() === 'PUT') {
        const body = route.request().postDataJSON()
        canvas.snapshot = body.snapshot
        canvas.version++
        return route.fulfill({ json: { version: canvas.version } })
      }
      return route.fulfill({ json: canvas })
    })
    await page.goto(`/?canvas=${canvasId}`)
    await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
    await swipe(page, { x: 100, y: 200 }, { x: 70, y: 80 })
    await expect.poll(() => canvas.snapshot.viewport.x).toBeCloseTo(70, 1)
    await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
    await page.reload()
    await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
    const restored = await readViewport(page)
    expect(restored.x).toBeCloseTo(70, 1)
    expect(restored.y).toBeCloseTo(80, 1)
  })
})
