import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { waitForTaskResult } from './task-result.mjs'

// 真实调用会使用方舟额度；不加入普通测试，必须显式开启，且最多提交一次。
if (process.env.RUN_LIVE_GENERATION !== '1') {
  throw new Error('仅在需要真实验证时设置 RUN_LIVE_GENERATION=1；普通测试使用模拟响应。')
}
const model = process.env.LIVE_IMAGE_MODEL || 'lite'
if (!['lite', 'pro'].includes(model)) throw new Error('LIVE_IMAGE_MODEL 只支持 lite 或 pro')
const expectedModel = model === 'pro' ? 'doubao-seedream-5-0-pro-260628' : 'doubao-seedream-5-0-260128'
const artifact = `artifacts/text-to-image-${model}-live`
const prompt = '星际穿越，黑洞，黑洞里冲出一辆快支离破碎的复古列车，抢视觉冲击力，电影大片，末日既视感，动感，对比色，oc渲染，光线追踪，动态模糊，景深，超现实主义，深蓝，画面通过细腻的丰富的色彩层次塑造主体与场景，质感真实，暗黑风背景的光影效果营造出氛围，整体兼具艺术幻想感，夸张的广角透视效果，耀光，反射，极致的光影，强引力，吞噬'
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 960 } })
let generationCalls = 0
page.on('request', (request) => {
  if (request.url().endsWith('/api/images/generations')) generationCalls++
})
try {
  await page.goto('http://127.0.0.1:5173/')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: /画布已保存|临时画布/ }).waitFor()
  await page.mouse.click(470, 160, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  const target = page.getByRole('article', { name: '图片节点 1', exact: true })
  await target.click({ position: { x: 80, y: 80 } })
  const editor = page.getByRole('dialog', { name: '图片提示词' })
  await editor.getByRole('button', { name: /^选择模型/ }).click()
  await page.getByRole('option', { name: `Doubao-Seedream-5.0-${model}`, exact: true }).click()
  await editor.getByRole('textbox').fill(prompt)
  const responsePromise = page.waitForResponse((response) => response.url().endsWith('/api/images/generations'), { timeout: 290_000 })
  const start = Date.now()
  await editor.getByRole('button', { name: '生成图片', exact: true }).click()
  console.log('已从前端提交一次真实文生图请求，等待模型返回。')
  const response = await responsePromise
  let body = await response.json()
  if (!response.ok()) throw new Error(`后端返回 ${response.status()}：${body.error?.code}，${body.error?.message}`)
  if (response.status() === 202) body = await waitForTaskResult(page.request, 'http://127.0.0.1:5173', body)
  if (body.storageError) throw new Error(body.storageError)
  if (body.model !== expectedModel) throw new Error('生成结果的模型与所选模型不符')
  await target.locator('img').waitFor({ timeout: 35_000 })
  await page.waitForFunction(() => {
    const image = document.querySelector('.media-node img')
    return image?.complete && image.naturalWidth > 0
  }, undefined, { timeout: 35_000 })
  const dimensions = await target.locator('img').evaluate((image) => ({ width: image.naturalWidth, height: image.naturalHeight }))
  if (generationCalls !== 1) throw new Error('请求数不符')
  await mkdir('artifacts', { recursive: true })
  await page.screenshot({ path: `${artifact}.png`, fullPage: true })
  const evidence = { verifiedAt: new Date().toISOString(), model: body.model, size: body.size, ...dimensions, seconds: Number(((Date.now() - start) / 1000).toFixed(1)), generationCalls, storedAssetKey: body.asset?.key, screenshot: `${artifact}.png` }
  await writeFile(`${artifact}.json`, JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} finally {
  await browser.close()
}
