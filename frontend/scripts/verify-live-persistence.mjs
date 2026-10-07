// Real browser -> real Go routes -> real PostgreSQL. Run the backend on 8081
// with TEST database, ARK_API_KEY=' ' and OSS_ENABLED=false: no paid/cloud calls.
import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import assert from 'node:assert/strict'

if (process.env.LIVE_PERSISTENCE_CHECK !== '1') throw new Error('Set LIVE_PERSISTENCE_CHECK=1 with the isolated verification backend first.')
const backend = process.env.PERSISTENCE_BACKEND || 'http://127.0.0.1:8081'
const browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' })
const errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  page.setDefaultTimeout(15000)
  page.on('pageerror', (error) => errors.push(error.message))
  let generationCalls = 0
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) return route.continue()
    if (url.pathname === '/api/images/generations') generationCalls++
    const response = await route.fetch({ url: `${backend}${url.pathname}${url.search}` })
    await route.fulfill({ response })
  })
  await page.goto('http://127.0.0.1:5173')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  const canvasId = new URL(page.url()).searchParams.get('canvas')
  assert(canvasId)
  await page.mouse.click(230, 150, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const node = page.getByRole('article', { name: '图片节点 1', exact: true })
  await node.click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox').fill('数据库验收：刷新后保留的草稿')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await page.reload()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await node.click({ position: { x: 60, y: 70 } })
  assert.equal(await page.getByRole('textbox').inputValue(), '数据库验收：刷新后保留的草稿')
  await page.getByRole('button', { name: '生成图片', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '尚未配置模型' }).waitFor({ timeout: 10000 })
  await page.getByRole('textbox').fill('生成结束后继续编辑的草稿')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await page.reload()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await page.getByRole('button', { name: '任务记录', exact: true }).click()
  const history = page.getByLabel('生成任务记录')
  await history.getByText('失败', { exact: true }).waitFor()
  await history.getByText('数据库验收：刷新后保留的草稿', { exact: true }).waitFor()
  const response = await page.request.get(`${backend}/api/canvases/${canvasId}`)
  assert(response.ok())
  const canvas = await response.json()
  assert.equal(canvas.snapshot.nodes[0].data.prompt, '生成结束后继续编辑的草稿')
  assert.equal(canvas.tasks.length, 1)
  assert.equal(canvas.tasks[0].error.code, 'MODEL_NOT_CONFIGURED')
  assert.equal(canvas.tasks[0].input.prompt, '数据库验收：刷新后保留的草稿')
  assert.equal(generationCalls, 1)
  assert.deepEqual(errors, [])
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: 'artifacts/persistence-live.png' })
  const evidence = { verifiedAt: new Date().toISOString(), canvasId, taskId: canvas.tasks[0].id, version: canvas.version, draftRestored: true, taskSnapshotRestored: true, taskStatus: canvas.tasks[0].status, generationRequests: generationCalls, paidModelCalls: 0, pageErrors: errors }
  await writeFile('artifacts/persistence-live.json', JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} finally { await browser.close() }
