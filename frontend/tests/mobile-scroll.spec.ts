import type { Page } from '@playwright/test'
import { test, expect } from './setup'

async function swipe(page: Page, x: number, y: number, xDistance: number, yDistance: number) {
  const client = await page.context().newCDPSession(page)
  try {
    await client.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] })
    for (let step = 1; step <= 10; step++) {
      await client.send('Input.dispatchTouchEvent', {
        type: 'touchMove', touchPoints: [{ x: x + xDistance * step / 10, y: y + yDistance * step / 10 }],
      })
      await new Promise(resolve => setTimeout(resolve, 16))
    }
    await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  } finally {
    await client.detach()
  }
}

async function swipePageToEdge(page: Page, selector: string, edge: 'top' | 'bottom') {
  const viewport = page.viewportSize()!
  const scroller = page.locator(selector)
  for (let attempt = 0; attempt < 12; attempt++) {
    const reached = await scroller.evaluate((element, target) => target === 'top'
      ? element.scrollTop <= 1
      : element.scrollTop + element.clientHeight >= element.scrollHeight - 1, edge)
    if (reached) return
    await swipe(page, viewport.width / 2, viewport.height * (edge === 'top' ? 0.3 : 0.8), 0, viewport.height * (edge === 'top' ? 0.55 : -0.55))
  }
  throw new Error(`未能通过触摸滑动到达页面${edge === 'top' ? '顶部' : '底部'}`)
}

for (const viewport of [{ width: 390, height: 844 }, { width: 320, height: 568 }]) {
  test.describe(`移动端 ${viewport.width}×${viewport.height}`, () => {
    test.use({ viewport, isMobile: true, hasTouch: true })

    test('首页画布列表可以向下和向上滑动', async ({ page }) => {
      const canvases = Array.from({ length: 5 }, (_, index) => ({
        id: `canvas-${index}`, title: `测试画布 ${index + 1}`, updatedAt: '2026-11-01T00:00:00Z', nodeCount: 0, preview: [],
      }))
      await page.route('**/api/persistence/config', route => route.fulfill({ json: { enabled: true } }))
      await page.route('**/api/canvases**', route => route.fulfill({ json: { canvases, hasMore: false } }))
      await page.goto('/')
      await expect(page.getByText('测试画布 5')).toBeAttached()

      const scroller = page.locator('.home-page')
      await swipe(page, viewport.width / 2, viewport.height * 0.8, 0, -viewport.height * 0.55)
      await expect.poll(() => scroller.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
      await swipePageToEdge(page, '.home-page', 'bottom')
      await expect(page.getByText('测试画布 5')).toBeInViewport()
      await swipePageToEdge(page, '.home-page', 'top')
      await expect(page.getByRole('heading', { name: '我的画布' })).toBeInViewport()
      expect(await page.locator('.public-shell').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    })

    test('提示词列表和图片区域支持触摸滑动', async ({ page }) => {
      const prompts = Array.from({ length: 4 }, (_, index) => ({
        id: `prompt-${index}`, title: `测试提示词 ${index + 1}`, content: '一段用于验证移动端滚动的提示词。',
        images: Array.from({ length: 3 }, (_, image) => ({ id: `${index}-${image}`, url: `/api/prompt-images/${index}-${image}` })),
        createdAt: '2026-11-01T00:00:00Z', updatedAt: '2026-11-01T00:00:00Z',
      }))
      await page.route('**/api/prompt-images/**', route => route.fulfill({ path: 'tests/fixtures/sample-image.png', contentType: 'image/png' }))
      await page.route('**/api/prompts?**', route => route.fulfill({ json: { prompts, hasMore: false } }))
      await page.goto('/?view=prompts')
      await expect(page.getByText('测试提示词 4')).toBeAttached()

      const scroller = page.locator('.prompt-page')
      const gallery = page.locator('.image-strip').first()
      const box = (await gallery.boundingBox())!
      const startY = Math.min(box.y + box.height / 2, viewport.height - 25)
      await swipe(page, box.x + box.width / 2, startY, 0, -Math.min(250, startY - 100))
      await expect.poll(() => scroller.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
      await swipePageToEdge(page, '.prompt-page', 'bottom')
      await expect(page.getByText('测试提示词 4')).toBeInViewport()
      await swipePageToEdge(page, '.prompt-page', 'top')
      await expect(page.getByRole('heading', { name: '提示词', exact: true })).toBeInViewport()

      const resetBox = (await gallery.boundingBox())!
      await swipe(page, resetBox.x + resetBox.width * 0.8, resetBox.y + resetBox.height / 2, -resetBox.width * 0.6, 0)
      await expect.poll(() => gallery.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)
      expect(await page.locator('.public-shell').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    })
  })
}
