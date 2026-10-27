// Real Chrome -> Vite -> Gin -> isolated PostgreSQL -> OAuth -> MCP.
// Requires local test DB and freshly built backend binaries; no provider calls.
import assert from 'node:assert/strict'
import { mkdir, stat, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { spawn, spawnSync } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import { chromium, expect } from '@playwright/test'

const root = fileURLToPath(new URL('..', import.meta.url)), backend = resolve(root, '../backend')
const artifacts = resolve(root, 'artifacts/mcp')
assert(process.env.TEST_DATABASE_URL, 'TEST_DATABASE_URL is required')
const database = new URL(process.env.TEST_DATABASE_URL)
assert.equal(database.hostname, '127.0.0.1'); assert.equal(database.pathname, '/frame_space_test')
const serverBinary = resolve(backend, 'bin/mcp-test-server.exe'), initBinary = resolve(backend, 'bin/mcp-test-set-admin.exe')
assert((await stat(serverBinary)).isFile()); assert((await stat(initBinary)).isFile())
const schema = `test_live_mcp_${Date.now()}`, origin = 'http://127.0.0.1:5179', callbackOrigin = 'http://127.0.0.1:5180'
database.search = ''; database.searchParams.set('search_path', schema)
const password = 'mcp test password', children = [], browserErrors = [], report = []
let browser, callbackURL
function sql(statement) {
  const result = spawnSync('wsl.exe', ['-d', 'Ubuntu-24.04', '-u', 'postgres', '--exec', 'psql', '-X', '-At', '-v', 'ON_ERROR_STOP=1', '-d', 'frame_space_test', '-c', statement], { encoding: 'utf8', windowsHide: true })
  assert.equal(result.status, 0, 'isolated SQL failed'); return result.stdout.trim()
}
function launch(command, args, cwd, env) {
  const child = spawn(command, args, { cwd, env: { ...process.env, ...env }, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.on('data', () => {}); child.stderr.on('data', () => {}); children.push(child)
}
async function ready(url) {
  const until = Date.now() + 25000
  while (Date.now() < until) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return } catch {}
    await new Promise(r => setTimeout(r, 200))
  }
  throw new Error('Local MCP services did not become ready')
}
const callback = createServer((request, response) => {
  const incoming = new URL(request.url, callbackOrigin)
  if (incoming.pathname !== '/callback') { response.writeHead(204); response.end(); return }
  callbackURL = incoming
  response.setHeader('Content-Type', 'text/html; charset=utf-8')
  response.end('<h1>授权回调已收到</h1>')
})
await new Promise((done, fail) => { callback.once('error', fail); callback.listen(5180, '127.0.0.1', done) })
await mkdir(artifacts, { recursive: true })
sql(`CREATE SCHEMA ${schema} AUTHORIZATION frame_space`)
try {
  launch(serverBinary, [], backend, { DATABASE_URL: database.toString(), PORT: '8083', APP_ORIGINS: origin, MCP_PUBLIC_URL: origin + '/mcp', AUTH_COOKIE_SECURE: 'false', OSS_ENABLED: 'false', ARK_API_KEY: '', GIN_MODE: 'release' })
  launch(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5179', '--strictPort'], root, { API_PROXY_TARGET: 'http://127.0.0.1:8083' })
  await Promise.all([ready(origin + '/admin/'), ready('http://127.0.0.1:8083/health')])
  async function signup(email) {
    const r = await fetch(origin + '/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: origin, 'X-Requested-With': 'frame-space' }, body: JSON.stringify({ email, password }) })
    assert.equal(r.status, 200); return r.json()
  }
  const operator = await signup('mcp-operator@example.test'); await signup('mcp-creator@example.test')
  const promoted = spawnSync(initBinary, ['--user-id', operator.user.id, '--apply'], { cwd: backend, env: { ...process.env, DATABASE_URL: database.toString() }, encoding: 'utf8', windowsHide: true })
  assert.equal(promoted.status, 0)
  const metadata = await (await fetch(origin + '/.well-known/oauth-authorization-server')).json()
  assert.equal(metadata.issuer, origin)
  const registration = await fetch(metadata.registration_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ client_name: 'MCP 浏览器验收应用', redirect_uris: [callbackOrigin + '/callback'], token_endpoint_auth_method: 'none', grant_types: ['authorization_code'] }) })
  assert.equal(registration.status, 201)
  const client = await registration.json(), verifier = randomBytes(32).toString('base64url')
  const authURL = new URL(metadata.authorization_endpoint)
  authURL.search = new URLSearchParams({ client_id: client.client_id, response_type: 'code', redirect_uri: callbackOrigin + '/callback', scope: 'users:read', resource: origin + '/mcp', state: randomBytes(16).toString('hex'), code_challenge_method: 'S256', code_challenge: createHash('sha256').update(verifier).digest('base64url') }).toString()
  browser = await chromium.launch({ channel: 'chrome', headless: true })
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } }), page = await context.newPage()
  page.on('pageerror', error => browserErrors.push(error.message))
  page.on('response', async response => {
    if (response.status() >= 400 && new URL(response.url()).pathname.startsWith('/oauth/')) {
      console.error(JSON.stringify({ path: new URL(response.url()).pathname, status: response.status(), origin: response.request().headers().origin }))
    }
  })
  await page.goto(authURL.toString())
  await expect(page.getByRole('heading', { name: '登录管理台', exact: true })).toBeVisible()
  await page.getByLabel('邮箱', { exact: true }).fill('mcp-operator@example.test')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录管理台', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权应用读取用户' })).toBeVisible()
  await expect(page.getByText('MCP 浏览器验收应用', { exact: true })).toBeVisible()
  await page.screenshot({ path: resolve(artifacts, 'consent-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await page.screenshot({ path: resolve(artifacts, 'consent-mobile.png'), fullPage: true })
  report.push('真实管理员登录后自动返回授权页；桌面和手机页面无横向溢出')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权回调已收到' })).toBeVisible()
  assert.equal(callbackURL.searchParams.get('error'), 'access_denied')
  assert.equal(sql(`SELECT count(*) FROM ${schema}.mcp_oauth_tokens`), '0')
  report.push('取消授权没有生成凭证')
  await page.goto(authURL.toString())
  await page.getByRole('button', { name: '授权读取用户', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权回调已收到' })).toBeVisible()
  assert.equal(callbackURL.searchParams.get('state'), authURL.searchParams.get('state'))
  assert.equal(callbackURL.searchParams.get('iss'), origin)
  const tokenResponse = await fetch(metadata.token_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams({ client_id: client.client_id, grant_type: 'authorization_code', code: callbackURL.searchParams.get('code'), redirect_uri: callbackOrigin + '/callback', code_verifier: verifier, resource: origin + '/mcp' }) })
  assert.equal(tokenResponse.status, 200); const token = await tokenResponse.json()
  let requestID = 0
  async function rpc(method, params) {
    const r = await fetch(origin + '/mcp', { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': '2025-06-18', Authorization: 'Bearer ' + token.access_token }, body: JSON.stringify({ jsonrpc: '2.0', id: ++requestID, method, params }) })
    assert.equal(r.status, 200); return r.json()
  }
  const initialized = await rpc('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'browser-acceptance', version: '1.0.0' } })
  assert.equal(initialized.result.serverInfo.name, 'bubu-users')
  const tools = await rpc('tools/list', {}); assert.equal(tools.result.tools.length, 1); assert.equal(tools.result.tools[0].name, 'list_users')
  const result = await rpc('tools/call', { name: 'list_users', arguments: { q: 'mcp-creator', limit: 20, offset: 0 } })
  assert.equal(result.result.structuredContent.total, 1); assert.equal(result.result.structuredContent.users[0].email, 'mcp-creator@example.test')
  report.push('真实 OAuth PKCE 兑换、旧版协议初始化、发现工具和用户查询通过')
  await page.goto(origin + '/admin/')
  await page.getByRole('link', { name: 'AI 应用授权' }).click()
  await expect(page.getByRole('heading', { name: '已授权的 AI 应用' })).toBeVisible()
  await page.screenshot({ path: resolve(artifacts, 'connections-mobile.png'), fullPage: true })
  await page.getByRole('button', { name: '撤销授权', exact: true }).click()
  await expect(page.getByText('目前没有有效的应用授权。')).toBeVisible()
  const rejected = await fetch(origin + '/mcp', { method: 'POST', headers: { Authorization: 'Bearer ' + token.access_token } })
  assert.equal(rejected.status, 401)
  report.push('管理台授权入口、撤销以及旧令牌即时拒绝通过')
  assert.deepEqual(browserErrors, [])
  assert.equal(sql(`SELECT count(*) FROM ${schema}.generation_tasks`), '0')
  await writeFile(resolve(artifacts, 'verification.json'), JSON.stringify({ passed: true, checks: report, browserErrors, fixture: 'isolated local PostgreSQL schema; removed after test', providerCalls: 0 }, null, 2))
  console.log(JSON.stringify({ passed: true, checks: report.length, providerCalls: 0 }))
} finally {
  await browser?.close()
  for (const child of children) child.kill()
  await Promise.all(children.map(child => child.exitCode !== null ? Promise.resolve() : new Promise(done => { child.once('exit', done); setTimeout(done, 3000) })))
  await new Promise(done => callback.close(done))
  sql(`DROP SCHEMA ${schema} CASCADE`)
}
