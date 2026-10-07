// Explicit real-browser acceptance: creates a separate canvas and uploads two
// synthetic fixtures. Generation requests are blocked; no provider call is made.
import { chromium, expect } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import assert from 'node:assert/strict'

if (process.env.RUN_LIVE_PROMPT_MENTIONS !== '1') throw new Error('Set RUN_LIVE_PROMPT_MENTIONS=1 to create an acceptance canvas and upload two synthetic fixtures.')
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL: 'http://127.0.0.1:5173', viewport: { width: 1680, height: 1000 } })
page.setDefaultTimeout(20000)
const errors = []
let generationRequests = 0
page.on('pageerror', (error) => errors.push(error.message))
await page.route('**/api/*/generations', (route) => { generationRequests++; return route.abort() })
const saved = () => expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
const editor = () => page.getByRole('textbox', { name: /^(图片|视频)提示词$/ })
const menu = () => page.getByRole('dialog', { name: '素材引用菜单', exact: true })
async function addMention(name, kind) {
  await editor().press('Control+End'); await editor().pressSequentially('@')
  await menu().getByRole('button', { name: `${kind}素材`, exact: true }).click()
  await menu().locator('.mention-submenu').getByRole('button', { name: `引用 ${name}`, exact: true }).click()
}
try {
  await page.goto('/')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await saved()
  const canvasId = new URL(page.url()).searchParams.get('canvas')
  assert(canvasId)
  await page.mouse.click(70, 145, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(['sample-image.png', 'reference-video.mp4'].map((name) => fileURLToPath(new URL(`../tests/fixtures/${name}`, import.meta.url))))
  await expect.poll(async () => {
    const response = await page.request.get(`/api/canvases/${canvasId}`)
    return (await response.json()).assets.length
  }, { timeout: 90000 }).toBe(2)
  await expect(page.getByText('保存中…', { exact: true })).toHaveCount(0)
  await page.mouse.click(1120, 145, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '视频', exact: true }).click()
  const target = page.getByRole('article', { name: '视频节点 1', exact: true })
  await target.click({ position: { x: 65, y: 75 } })
  await editor().fill('参考 ')
  await addMention('sample-image.png', '图片')
  await editor().pressSequentially('的颜色，结合 ')
  await addMention('reference-video.mp4', '视频')
  await editor().pressSequentially('的镜头运动。\n保持主体清晰。')
  await expect(editor().locator('[data-prompt-reference]')).toHaveText(['图片1', '视频1'])
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await saved()
  const canvas = await page.request.get(`/api/canvases/${canvasId}`).then((response) => response.json())
  const targetSnapshot = canvas.snapshot.nodes.find((node) => node.data.kind === 'video' && node.data.origin !== 'upload')
  assert.equal(targetSnapshot.data.referenceIds.length, 2)
  const references = targetSnapshot.data.promptParts.filter((part) => part.type === 'reference')
  assert.deepEqual(references.map((part) => part.nodeId), targetSnapshot.data.referenceIds)
  assert.equal(canvas.tasks.length, 0)
  assert.equal(targetSnapshot.data.prompt, '参考 @sample-image.png 的颜色，结合 @reference-video.mp4 的镜头运动。\n保持主体清晰。')
  await page.reload(); await saved()
  await expect(page.locator('.vue-flow__edge')).toHaveCount(2)
  await target.click({ position: { x: 65, y: 75 } })
  await expect(editor().locator('[data-prompt-reference]')).toHaveText(['图片1', '视频1'])
  await expect(editor()).toContainText('保持主体清晰。')
  await editor().press('Control+End'); await editor().pressSequentially('@')
  await expect(menu().locator('.mention-main').getByRole('button', { name: /^引用 / })).toHaveCount(2)
  await menu().getByRole('button', { name: '图片素材', exact: true }).click()
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: 'artifacts/prompt-mentions-live-menu.png', fullPage: true })
  await editor().press('Escape'); await editor().press('Backspace')
  await page.getByRole('button', { name: '移除参考 sample-image.png', exact: true }).click()
  await expect(editor().locator('[data-prompt-reference]').nth(0)).toHaveClass(/is-invalid/)
  await addMention('sample-image.png', '图片')
  await expect(editor().locator('[data-prompt-reference]')).toHaveText(['图片1', '视频1', '图片1'])
  await page.getByRole('button', { name: '保存', exact: true }).click(); await saved()
  await page.screenshot({ path: 'artifacts/prompt-mentions-live.png', fullPage: true })
  assert.equal(generationRequests, 0)
  assert.deepEqual(errors, [])
  const evidence = { verifiedAt: new Date().toISOString(), canvasId, canvasURL: page.url(), uploadCount: 2, databaseRestored: true, referencesRestored: true, promptPartsRestored: true, invalidReferenceRepair: true, generationRequests, pageErrors: errors, screenshots: ['artifacts/prompt-mentions-live-menu.png', 'artifacts/prompt-mentions-live.png'] }
  await writeFile('artifacts/prompt-mentions-live.json', JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} catch (error) {
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: 'artifacts/prompt-mentions-live-failure.png', fullPage: true }).catch(() => {})
  throw error
} finally { await browser.close() }
