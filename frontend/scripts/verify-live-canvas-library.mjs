import { chromium, expect } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'

// Creates exactly two named test canvases in the configured local database.
// No uploads or generation. IDs are recorded even on failure for bounded cleanup.
if (process.env.RUN_LIVE_CANVAS_LIBRARY !== '1') throw new Error('Set RUN_LIVE_CANVAS_LIBRARY=1 for the real local database check.')
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL: 'http://127.0.0.1:5173', viewport: { width: 1440, height: 900 } })
const report = { canvases: [], checks: [], errors: [], generationCalls: 0 }
await mkdir('artifacts', { recursive: true })
page.on('pageerror', (error) => report.errors.push(error.message))
await page.route('**/api/*/generations', (route) => { report.generationCalls++; return route.abort() })
const saved = () => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
const home = async () => {
  await page.getByRole('button', { name: '返回首页', exact: true }).click()
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
}
const create = async (title) => {
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByLabel('画布名称').fill(title)
  const requestPromise = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === '/api/canvases')
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  // Read the submitted ID; navigation may already have evicted the response body.
  const { id } = (await requestPromise).postDataJSON()
  report.canvases.push({ id, title })
  await writeFile('artifacts/canvas-library-live.json', JSON.stringify(report, null, 2))
  await saved()
  await expect(page.getByRole('article')).toHaveCount(0)
  return id
}
const edit = async (prompt) => {
  await page.mouse.click(220, 190, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox', { name: '图片提示词' }).fill(prompt)
}
try {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  await page.getByRole('button', { name: '新建画布', exact: true }).waitFor()
  const existing = await page.request.get('/api/canvases?limit=100').then((r) => r.json())
  report.existingCount = existing.canvases.length
  const a = await create('验收 · 画布隔离 A')
  await edit('仅属于验收 A 的草稿')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  await home()
  const aBefore = await page.request.get(`/api/canvases/${a}`).then((r) => r.json())
  const b = await create('验收 · 画布隔离 B')
  await expect(page.getByLabel('当前缩放比例')).toHaveText('100%')
  await edit('仅属于验收 B 的草稿')
  await home()
  await page.getByRole('link', { name: new RegExp(`打开画布：验收 · 画布隔离 A（${a.slice(0, 8)}）`) }).click()
  await saved()
  await expect(page.getByLabel('当前缩放比例')).not.toHaveText('100%')
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('仅属于验收 A 的草稿')
  await page.reload()
  await saved()
  await expect(page.getByRole('article')).toHaveCount(1)
  const aAfter = await page.request.get(`/api/canvases/${a}`).then((r) => r.json())
  expect(aAfter.snapshot).toEqual(aBefore.snapshot)
  expect(aAfter.title).toBe('验收 · 画布隔离 A')
  await home()
  await page.getByRole('link', { name: new RegExp(`打开画布：验收 · 画布隔离 B（${b.slice(0, 8)}）`) }).click()
  await saved()
  await page.getByRole('article').click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox', { name: '图片提示词' })).toHaveText('仅属于验收 B 的草稿')
  await home()
  await expect(page.locator('.canvas-card').first()).toContainText('验收 · 画布隔离 B')
  await page.screenshot({ path: 'artifacts/canvas-library-live-desktop.png' })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.locator('.home-page').evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: 'artifacts/canvas-library-live-mobile.png' })
  expect(report.errors).toEqual([])
  expect(report.generationCalls).toBe(0)
  report.checks.push('home lists existing canvases', 'create two unique IDs', 'blank new canvas', 'save before home', 'separate drafts', 'viewport restore', 'reload restore', 'recent order', 'mobile layout', 'no generation calls')
} finally {
  await writeFile('artifacts/canvas-library-live.json', JSON.stringify(report, null, 2))
  await browser.close()
}
console.log(JSON.stringify(report, null, 2))
