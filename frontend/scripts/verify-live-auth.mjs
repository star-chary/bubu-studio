// Real Chrome -> Vite -> Gin -> PostgreSQL acceptance. Uses a disposable schema
// in frame_space_test; cloud storage and model credentials are explicitly disabled.
import assert from 'node:assert/strict'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { spawn, spawnSync } from 'node:child_process'
import { chromium } from '@playwright/test'

const frontend = fileURLToPath(new URL('..', import.meta.url))
const backend = resolve(frontend, '../backend')
const artifacts = resolve(frontend, 'artifacts')
await mkdir(artifacts, { recursive: true })
const envText = await readFile(resolve(backend, '.env.local'), 'utf8')
const databaseLine = envText.split(/\r?\n/).find(line => line.startsWith('DATABASE_URL='))
assert(databaseLine, 'DATABASE_URL required')
const database = new URL(databaseLine.slice('DATABASE_URL='.length).trim())
assert.equal(database.hostname, '127.0.0.1', 'acceptance only uses local PostgreSQL')
assert(['/frame_space', '/frame_space_test'].includes(database.pathname))
database.pathname = '/frame_space_test'
const schema = `test_live_auth_${Date.now()}`
database.searchParams.set('search_path', schema)
function sql(statement) {
  const result = spawnSync('wsl.exe', ['-d', 'Ubuntu-24.04', '-u', 'postgres', '--exec', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-d', 'frame_space_test', '-c', statement], { encoding: 'utf8', windowsHide: true })
  assert.equal(result.status, 0, 'isolated test schema operation failed')
}
sql(`CREATE SCHEMA ${schema} AUTHORIZATION frame_space`)
const children = []
function launch(command, args, cwd, env) {
  const child = spawn(command, args, { cwd, env: { ...process.env, ...env }, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.on('data', () => {})
  child.stderr.on('data', () => {})
  children.push(child)
  return child
}
async function ready(url) {
  const end = Date.now() + 25_000
  while (Date.now() < end) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return } catch {}
    await new Promise(r => setTimeout(r, 200))
  }
  throw new Error(`test service did not start: ${url}`)
}
let browser
try {
  launch(resolve(backend, 'bin/server.exe'), [], backend, { DATABASE_URL: database.toString(), PORT: '8081', APP_ORIGINS: 'http://127.0.0.1:5175', AUTH_COOKIE_SECURE: 'false', OSS_ENABLED: 'false', ARK_API_KEY: '' })
  launch(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5175', '--strictPort'], frontend, { API_PROXY_TARGET: 'http://127.0.0.1:8081' })
  await Promise.all([ready('http://127.0.0.1:8081/health'), ready('http://127.0.0.1:5175')])
  browser = await chromium.launch({ channel: 'chrome', headless: true })
  const a = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const b = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const page = await a.newPage()
  const failures = []
  page.on('pageerror', e => failures.push(e.message))
  await page.goto('http://127.0.0.1:5175')
  await page.getByRole('heading', { name: '进入你的工作空间' }).waitFor()
  await page.screenshot({ path: resolve(artifacts, 'auth-login-desktop.png'), fullPage: true })
  const emailA = 'acceptance-a@custom.example'
  const emailB = 'acceptance-b@different.example'
  const password = 'local-test-pass123'
  async function login(page, email) {
    await page.getByLabel('邮箱', { exact: true }).fill(email)
    await page.getByLabel('密码', { exact: true }).fill(password)
    await page.getByRole('button', { name: '继续', exact: true }).click()
    await page.getByRole('heading', { name: '我的画布' }).waitFor()
  }
  await login(page, emailA)
  const cookie = (await a.cookies()).find(c => c.name === 'frame_session')
  assert(cookie?.httpOnly && cookie.sameSite === 'Lax')
  assert.equal(await page.evaluate(() => document.cookie.includes('frame_session')), false)
  const me = await (await a.request.get('http://127.0.0.1:5175/api/auth/me')).json()
  assert.equal(me.user.email, emailA)
  await page.getByRole('button', { name: /新建画布/ }).click()
  await page.getByRole('textbox', { name: '画布名称' }).fill('账号隔离验收')
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  const canvasURL = page.url()
  const canvasID = new URL(canvasURL).searchParams.get('canvas')
  assert(canvasID)
  await page.mouse.click(230, 230, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox').fill('真实数据库保存后的私有草稿')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await page.reload()
  await page.getByLabel('画布保存状态').filter({ hasText: '画布已保存' }).waitFor()
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  assert.equal(await page.getByRole('textbox').innerText(), '真实数据库保存后的私有草稿')
  await page.keyboard.press('Escape')
  await page.screenshot({ path: resolve(artifacts, 'auth-canvas-desktop.png'), fullPage: true })
  const pageB = await b.newPage()
  await pageB.goto('http://127.0.0.1:5175')
  await pageB.getByRole('heading', { name: '进入你的工作空间' }).waitFor()
  await pageB.screenshot({ path: resolve(artifacts, 'auth-login-mobile.png'), fullPage: true })
  await login(pageB, emailB)
  const listB = await (await b.request.get('http://127.0.0.1:5175/api/canvases')).json()
  assert.equal(listB.canvases.length, 0)
  assert.equal((await b.request.get(`http://127.0.0.1:5175/api/canvases/${canvasID}`)).status(), 404)
  await pageB.goto(canvasURL)
  await pageB.getByText('画布、节点或任务不存在，请先保存画布。', { exact: true }).waitFor()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.getByRole('heading', { name: '进入你的工作空间' }).waitFor()
  const revoked = await fetch('http://127.0.0.1:8081/api/auth/me', { headers: { Cookie: `${cookie.name}=${cookie.value}` } })
  assert.equal(revoked.status, 401)
  await page.getByLabel('邮箱', { exact: true }).fill(emailA)
  await page.getByLabel('密码', { exact: true }).fill('incorrect password')
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '邮箱或密码不正确' }).waitFor()
  await login(page, emailA)
  await page.getByRole('link', { name: /打开画布：账号隔离验收/ }).waitFor()
  assert.equal((await (await a.request.get('http://127.0.0.1:5175/api/auth/me')).json()).user.id, me.user.id)
  assert.deepEqual(failures, [])
  const report = { passed: true, checkedAt: new Date().toISOString(), checks: ['email auto-registration', 'existing-account password validation', 'HttpOnly cookie', 'refresh session restoration', 'canvas save and reload', 'two-account isolation', 'logout server revocation', 'same account relogin'], realPostgres: true, modelCalls: 0, storageCalls: 0, isolatedSchema: schema }
  await writeFile(resolve(artifacts, 'auth-live.json'), JSON.stringify(report, null, 2))
  console.log(JSON.stringify(report))
} finally {
  await browser?.close()
  for (const child of children) {
    if (child.exitCode === null) child.kill()
    await new Promise(resolve => { if (child.exitCode !== null) return resolve(); child.once('exit', resolve); setTimeout(resolve, 2000) })
  }
  sql(`DROP SCHEMA ${schema} CASCADE`)
}
