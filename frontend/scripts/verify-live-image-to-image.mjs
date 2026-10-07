import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { waitForTaskResult } from './task-result.mjs'

// Creates one uploaded reference and one generated image. Never auto-retries.
if (process.env.RUN_LIVE_GENERATION !== '1' || process.env.RUN_LIVE_STORAGE !== '1') {
  throw new Error('真实图生图验收需显式设置 RUN_LIVE_GENERATION=1 和 RUN_LIVE_STORAGE=1')
}
const model = process.env.LIVE_IMAGE_MODEL || 'lite'
if (!['lite', 'pro'].includes(model)) throw new Error('LIVE_IMAGE_MODEL 只支持 lite 或 pro')
const modelId = model === 'lite' ? 'doubao-seedream-5-0-260128' : 'doubao-seedream-5-0-pro-260628'
const baseURL = 'http://127.0.0.1:5173'
const config = await fetch(`${baseURL}/api/storage/config`).then(r => r.json())
if (!config.enabled) throw new Error('图生图验收需要启用 OSS')
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL, viewport: { width: 1440, height: 960 } })
const artifact = `artifacts/image-to-image-${model}-live`
let generationCalls = 0
page.on('request', request => { if (new URL(request.url()).pathname === '/api/images/generations') generationCalls++ })
await page.addInitScript(() => {
  window.__liveImageToImage = {}
  const originalFetch = window.fetch
  window.fetch = async function (...args) {
    const response = await originalFetch.apply(this, args)
    const path = new URL(response.url).pathname
    if (path === '/api/assets' || path === '/api/images/generations') {
      const name = path === '/api/assets' ? 'upload' : 'generation'
      window.__liveImageToImage[name] = { status: response.status, body: await response.clone().json() }
    }
    return response
  }
})
try {
  await page.goto('/')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: /画布已保存|临时画布/ }).waitFor()
  await page.mouse.click(60, 160, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles(fileURLToPath(new URL('../tests/fixtures/sample-image.png', import.meta.url)))
  const reference = page.getByRole('article', { name: 'sample-image.png', exact: true })
  await page.waitForFunction(() => !!window.__liveImageToImage.upload, undefined, { timeout: 130_000 })
  await reference.getByText('保存中…', { exact: true }).waitFor({ state: 'hidden', timeout: 130_000 })
  await page.mouse.click(810, 160, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const target = page.getByRole('article', { name: '图片节点 1', exact: true })
  await target.click({ position: { x: 60, y: 70 } })
  const editor = page.getByRole('dialog')
  await editor.getByRole('button', { name: '参考', exact: true }).click()
  await reference.click({ position: { x: 60, y: 70 } })
  await page.keyboard.press('Escape')
  await editor.getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: `Doubao-Seedream-5.0-${model}`, exact: true }).click()
  await editor.getByRole('textbox').fill('以参考图为基础，保留竖向色条和斜线的构图，把强烈的红绿蓝颜色改成柔和的粉蓝、浅粉和薄荷绿色，呈现细腻的水彩纸纹理。去除数字文字，仅生成一张图片。')
  const start = Date.now()
  await editor.getByRole('button', { name: '生成图片', exact: true }).click()
  console.log('已提交一次真实图生图请求，等待模型生成和 OSS 转存。')
  await page.waitForFunction(() => !!window.__liveImageToImage.generation, undefined, { timeout: 320_000 })
  const captured = await page.evaluate(() => window.__liveImageToImage)
  const { status } = captured.generation
  let { body } = captured.generation
  if (status !== 200 && status !== 202) throw new Error(`生成失败：HTTP ${status} / ${body.error?.code} / ${body.error?.message}`)
  if (status === 202) body = await waitForTaskResult(page.request, baseURL, body)
  if (body.storageError || !body.asset) throw new Error(body.storageError || '生成结果尚未转存')
  if (body.model !== modelId || generationCalls !== 1) throw new Error('模型或生成请求数量不符')
  await target.locator('img').waitFor({ timeout: 35_000 })
  await page.waitForFunction(() => [...document.querySelectorAll('.media-node img')].every(img => img.complete && img.naturalWidth > 0), undefined, { timeout: 35_000 })
  const dimensions = await target.locator('img').evaluate(img => ({ width: img.naturalWidth, height: img.naturalHeight }))
  const result = await page.request.get(body.asset.url)
  if (!result.ok() || (await result.body()).length !== body.asset.bytes) throw new Error('转存后的图片读取失败')
  if (await page.getByRole('article').count() !== 2 || await page.locator('.vue-flow__edge').count() !== 1) throw new Error('结果应回填同一节点，保留参考连接')
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: `${artifact}.png`, fullPage: true })
  const evidence = {
    verifiedAt: new Date().toISOString(), model: body.model, generationCalls, referenceCount: 1,
    sourceAssetKey: captured.upload.body.key, storedAssetKey: body.asset.key,
    ...dimensions, bytes: body.asset.bytes, seconds: Number(((Date.now() - start) / 1000).toFixed(1)),
    storedResultReadable: true, screenshot: `${artifact}.png`,
  }
  await writeFile(`${artifact}.json`, JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} finally { await browser.close() }
