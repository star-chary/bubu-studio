// Real Chrome -> independent admin/canvas Vite servers -> Gin -> PostgreSQL.
// Only a disposable schema in the explicitly configured local test DB is used.
import assert from 'node:assert/strict'
import { mkdir, stat, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { spawn, spawnSync } from 'node:child_process'
import { createServer } from 'node:net'
import { chromium, expect } from '@playwright/test'

const adminRoot = fileURLToPath(new URL('..', import.meta.url))
const backend = resolve(adminRoot, '../backend'), frontend = resolve(adminRoot, '../frontend')
const artifacts = resolve(adminRoot, 'artifacts')
assert(process.env.TEST_DATABASE_URL, 'TEST_DATABASE_URL is required')
assert(process.env.ADMIN_TEST_BACKEND_BIN, 'A freshly compiled ADMIN_TEST_BACKEND_BIN is required')
assert(process.env.ADMIN_TEST_INIT_BIN, 'A freshly compiled ADMIN_TEST_INIT_BIN is required')
const database = new URL(process.env.TEST_DATABASE_URL)
assert(['postgres:', 'postgresql:'].includes(database.protocol))
assert.equal(database.hostname, '127.0.0.1')
assert.equal(database.pathname, '/frame_space_test')
const backendBinary = resolve(backend, process.env.ADMIN_TEST_BACKEND_BIN)
const initBinary = resolve(backend, process.env.ADMIN_TEST_INIT_BIN)
assert((await stat(backendBinary)).isFile()); assert((await stat(initBinary)).isFile())
const schema = `test_live_admin_${Date.now()}`
database.search = ''; database.searchParams.set('search_path', schema)
await mkdir(artifacts, { recursive: true })
for (const port of [8082, 5177, 5178]) {
  await new Promise((done, fail) => { const server = createServer(); server.once('error', fail); server.listen(port, '127.0.0.1', () => server.close(done)) })
}
function sql(statement) {
  const result = spawnSync('wsl.exe', ['-d', 'Ubuntu-24.04', '-u', 'postgres', '--exec', 'psql', '-X', '-At', '-v', 'ON_ERROR_STOP=1', '-d', 'frame_space_test', '-c', statement], { encoding: 'utf8', windowsHide: true })
  assert.equal(result.status, 0, 'isolated schema SQL failed')
  return result.stdout.trim()
}
sql(`CREATE SCHEMA ${schema} AUTHORIZATION frame_space`)
const children = []
function launch(command, args, cwd, env) {
  const child = spawn(command, args, { cwd, env: { ...process.env, ...env }, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.on('data', () => {}); child.stderr.on('data', () => {}); children.push(child)
}
async function ready(url) {
  const deadline = Date.now() + 25000
  while (Date.now() < deadline) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return } catch {}
    await new Promise(r => setTimeout(r, 200))
  }
  throw new Error(`service did not become ready: ${url}`)
}
const adminURL = 'http://127.0.0.1:5177/admin/'
const origin = 'http://127.0.0.1:5177', canvasOrigin = 'http://127.0.0.1:5178'
const password = 'admin test password'
let browser
try {
  launch(backendBinary, [], backend, { DATABASE_URL: database.toString(), PORT: '8082', APP_ORIGINS: `${origin},${canvasOrigin}`, AUTH_COOKIE_SECURE: 'false', OSS_ENABLED: 'false', ARK_API_KEY: '', GIN_MODE: 'release' })
  launch(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5177', '--strictPort'], adminRoot, { API_PROXY_TARGET: 'http://127.0.0.1:8082' })
  launch(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5178', '--strictPort'], frontend, { API_PROXY_TARGET: 'http://127.0.0.1:8082' })
  await Promise.all([ready(`${origin}/admin/`), ready(`${canvasOrigin}/`), ready('http://127.0.0.1:8082/health')])
  browser = await chromium.launch({ channel: 'chrome', headless: true })
  const adminContext = await browser.newContext({ viewport: { width: 1440, height: 960 } })
  const userContext = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const setupContext = await browser.newContext()
  async function signup(context, email) {
    const response = await context.request.post(`${canvasOrigin}/api/auth/login`, { data: { email, password }, headers: { Origin: canvasOrigin, 'X-Requested-With': 'frame-space' } })
    assert.equal(response.status(), 200)
    return response.json()
  }
  const adminAccount = await signup(setupContext, 'operator@example.test')
  const userAccount = await signup(userContext, 'creator@example.test')
  // Additional local fixtures exercise a real second page and account-without-balance reads.
  sql(`INSERT INTO ${schema}.users(id,email,password_hash) SELECT gen_random_uuid(),'tester-'||i||'@example.test','unused' FROM generate_series(1,21) AS i`)
  const preview = spawnSync(initBinary, ['--user-id', adminAccount.user.id], { cwd: backend, env: { ...process.env, DATABASE_URL: database.toString() }, encoding: 'utf8', windowsHide: true })
  assert.equal(preview.status, 0)
  assert.equal(sql(`SELECT role FROM ${schema}.users WHERE id='${adminAccount.user.id}'`), 'user')
  const initialize = spawnSync(initBinary, ['--user-id', adminAccount.user.id, '--apply'], { cwd: backend, env: { ...process.env, DATABASE_URL: database.toString() }, encoding: 'utf8', windowsHide: true })
  assert.equal(initialize.status, 0)
  assert.equal((await setupContext.request.get(`${canvasOrigin}/api/auth/me`)).status(), 401)
  assert.equal((await userContext.request.get(`${origin}/api/admin/users`)).status(), 403)
  const beforeCount = sql(`SELECT count(*) FROM ${schema}.users`)
  const unknown = await adminContext.request.post(`${origin}/api/admin/auth/login`, { data: { email: 'unknown@example.test', password }, headers: { Origin: origin, 'X-Requested-With': 'frame-space' } })
  assert.equal(unknown.status(), 401)
  assert.equal(sql(`SELECT count(*) FROM ${schema}.users`), beforeCount)

  const page = await adminContext.newPage(), failures = []
  page.on('pageerror', e => failures.push(e.message))
  await page.goto(adminURL)
  await expect(page.getByRole('heading', { name: '登录管理台', exact: true })).toBeVisible()
  assert.equal(await page.title(), 'BuBu-后台管理')
  await page.screenshot({ path: resolve(artifacts, 'admin-login-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await page.screenshot({ path: resolve(artifacts, 'admin-login-mobile.png'), fullPage: true })
  await page.setViewportSize({ width: 1440, height: 960 })
  await page.getByLabel('邮箱', { exact: true }).fill('operator@example.test')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录管理台', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  const cookie = (await adminContext.cookies()).find(c => c.name === 'frame_session')
  assert(cookie?.httpOnly && cookie.path === '/' && cookie.sameSite === 'Lax')
  await page.screenshot({ path: resolve(artifacts, 'admin-users-desktop.png'), fullPage: true })
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByText('第 2 页', { exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  await page.getByLabel('搜索邮箱或用户 ID').fill('creator@example.test')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.getByRole('table')).toContainText('creator@example.test')
  async function grant(points, reason) {
    await page.getByRole('button', { name: '发放积分', exact: true }).click()
    await page.getByLabel('增加积分', { exact: true }).fill(String(points))
    await page.getByLabel('发放原因', { exact: true }).fill(reason)
    await page.getByRole('button', { name: '核对并发放' }).click()
    await expect(page.getByText('请核对本次发放', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '确认发放', exact: true }).click()
  }
  await grant(50, '真实链路体验测试补充')
  await expect(page.getByRole('dialog').getByRole('status').filter({ hasText: '发放成功' })).toBeVisible()
  await expect(page.getByText('操作人：operator@example.test')).toBeVisible()
  await page.screenshot({ path: resolve(artifacts, 'admin-grant-desktop.png'), fullPage: true })
  const balance = async () => (await userContext.request.get(`${canvasOrigin}/api/credits`)).json()
  assert.deepEqual(await balance(), { available: 250, reserved: 0 })
  await page.getByRole('button', { name: '关闭用户详情' }).click()

  let attempts = 0
  const requestIDs = []
  await page.route(`**/api/admin/users/${userAccount.user.id}/credits`, async route => {
    requestIDs.push(route.request().postDataJSON().requestId)
    const response = await route.fetch(); assert.equal(response.status(), 200)
    if (++attempts === 1) await route.abort('failed')
    else await route.fulfill({ response })
  })
  await grant(25, '响应丢失后的重试验收')
  await expect(page.getByText('发放结果待确认', { exact: true })).toBeVisible()
  assert.deepEqual(await balance(), { available: 275, reserved: 0 })
  await page.reload()
  await page.getByRole('button', { name: '查看并重试' }).click()
  await page.getByRole('button', { name: '确认结果 / 重试' }).click()
  await expect(page.getByRole('dialog').getByRole('status').filter({ hasText: '没有重复增加' })).toBeVisible()
  await expect(page.getByText('正在读取记录…', { exact: true })).toHaveCount(0)
  assert.equal(requestIDs.length, 2); assert.equal(requestIDs[0], requestIDs[1])
  assert.deepEqual(await balance(), { available: 275, reserved: 0 })
  assert.equal(sql(`SELECT count(*) FROM ${schema}.credit_ledger WHERE user_id='${userAccount.user.id}' AND source='manual'`), '2')
  await page.setViewportSize({ width: 390, height: 844 })
  assert(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= innerWidth))
  await page.screenshot({ path: resolve(artifacts, 'admin-grant-mobile.png'), fullPage: true })
  await page.getByRole('button', { name: '关闭用户详情' }).click()
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await page.screenshot({ path: resolve(artifacts, 'admin-users-mobile.png'), fullPage: true })
  // The existing canvas UI reads the same account after the admin grant.
  const userPage = await userContext.newPage()
  await userPage.goto(canvasOrigin)
  await expect(userPage.getByRole('button', { name: '查看积分余额和流水' }).getByText('积分 275', { exact: true })).toBeVisible()
  const csrfFailure = await adminContext.request.post(`${origin}/api/admin/users/${userAccount.user.id}/credits`, { data: { points: 1, reason: 'reject', requestId: crypto.randomUUID() }, headers: { Origin: origin } })
  assert.equal(csrfFailure.status(), 403)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '登录管理台', exact: true })).toBeVisible()
  assert.equal((await adminContext.request.get(`${origin}/api/admin/users`)).status(), 401)
  const revoked = await fetch(`${origin}/api/admin/me`, { headers: { Cookie: `${cookie.name}=${cookie.value}` } })
  assert.equal(revoked.status, 401)
  assert.deepEqual(failures, [])
  const report = { passed: true, checkedAt: new Date().toISOString(), checks: ['admin initialization preview and explicit promotion', 'old sessions revoked', 'unknown admin does not auto-register', 'ordinary user denied', 'admin HttpOnly session login', 'real user pagination and search', 'manual grant and operator/reason audit', 'response lost after commit: same request ID survives refresh and grants once', 'canvas reads updated 275 credits', 'CSRF rejection', 'logout revocation', 'desktop and mobile rendering'], realPostgres: true, modelCalls: 0, storageCalls: 0, isolatedSchema: schema }
  await writeFile(resolve(artifacts, 'admin-live.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
} finally {
  await browser?.close()
  for (const child of children) {
    if (child.exitCode === null) child.kill()
    await new Promise(done => { if (child.exitCode !== null) return done(); child.once('exit', done); setTimeout(done, 2000) })
  }
  sql(`DROP SCHEMA ${schema} CASCADE`)
}
