import { chromium } from '@playwright/test'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'

// This probe writes two small test files to the configured bucket. It does not
// generate images or delete objects, and never reads cloud credentials.
if (process.env.RUN_LIVE_STORAGE !== '1') throw new Error('真实存储验收需显式设置 RUN_LIVE_STORAGE=1')
const baseURL = 'http://127.0.0.1:5173'
const config = await fetch(`${baseURL}/api/storage/config`).then((response) => response.json())
if (!config.enabled) throw new Error('后端尚未启用 OSS，请完成配置并重启后端')
const imagePath = fileURLToPath(new URL('../tests/fixtures/sample-image.png', import.meta.url))
const videoPath = fileURLToPath(new URL('../tests/fixtures/sample-video.mp4', import.meta.url))
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage({ baseURL, viewport: { width: 1440, height: 900 } })
const results = []
let uploadCalls = 0
page.on('request', (request) => { if (new URL(request.url()).pathname === '/api/assets') uploadCalls++ })
// Capture a clone while the app consumes the real response. Chrome may evict
// response bodies from its inspector cache before Playwright reads them later.
await page.addInitScript(() => {
  window.__liveStorageResponses = []
  const originalFetch = window.fetch
  window.fetch = async function (...args) {
    const response = await originalFetch.apply(this, args)
    if (new URL(response.url).pathname === '/api/assets') {
      window.__liveStorageResponses.push({ status: response.status, asset: await response.clone().json() })
    }
    return response
  }
})
try {
  await page.goto('/')
  await page.getByRole('button', { name: '新建画布', exact: true }).click()
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: /画布已保存|临时画布/ }).waitFor()
  await page.mouse.click(160, 160, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  await (await chooser).setFiles([imagePath, videoPath])
  await page.waitForFunction(() => window.__liveStorageResponses.length === 2, undefined, { timeout: 135_000 })
  await page.getByText('保存中…', { exact: true }).waitFor({ state: 'hidden', timeout: 135_000 })
  const responses = await page.evaluate(() => window.__liveStorageResponses)
  if (uploadCalls !== 2 || responses.length !== 2) throw new Error('实际上传次数不符')
  await mkdir('artifacts', { recursive: true })
  await writeFile('artifacts/storage-live-uploaded.json', JSON.stringify({ uploadedAt: new Date().toISOString(), responses }, null, 2))
  for (const { status, asset } of responses) {
    if (status !== 201) throw new Error(`上传失败：HTTP ${status}`)
    const bytes = await readFile(asset.kind === 'images' ? imagePath : videoPath)
    const full = await page.request.get(asset.url)
    const digest = (data) => createHash('sha256').update(data).digest('hex')
    if (!full.ok() || digest(await full.body()) !== digest(bytes)) throw new Error('从 OSS 读取的内容与原文件不同')
    if (asset.kind === 'videos') {
      const partial = await page.request.get(asset.url, { headers: { Range: 'bytes=0-31' } })
      if (partial.status() !== 206 || !(await partial.body()).equals(bytes.subarray(0, 32))) throw new Error('视频 Range 读取未通过')
    }
    results.push({ key: asset.key, kind: asset.kind, bytes: asset.bytes, canvasId: asset.canvasId, nodeId: asset.nodeId, contentVerified: true })
  }
  await page.screenshot({ path: 'artifacts/storage-live.png', fullPage: true })
  const evidence = { verifiedAt: new Date().toISOString(), uploadCalls, results, screenshot: 'artifacts/storage-live.png' }
  await writeFile('artifacts/storage-live.json', JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} finally {
  await browser.close()
}
