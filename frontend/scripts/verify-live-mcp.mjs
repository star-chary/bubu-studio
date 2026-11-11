// Real Chrome -> Vite -> Go -> isolated PostgreSQL -> OAuth -> MCP -> canvas.
// Requires TEST_DATABASE_URL for the local frame_space_test database and a
// freshly built MCP_TEST_BACKEND_BIN. Does not call models or cloud storage.
import assert from 'node:assert/strict'
import { randomBytes, randomUUID, createHash } from 'node:crypto'
import { spawn, spawnSync } from 'node:child_process'
import { stat } from 'node:fs/promises'
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { chromium, expect } from '@playwright/test'

const frontend = fileURLToPath(new URL('..', import.meta.url))
const backend = resolve(frontend, '../backend')
assert(process.env.TEST_DATABASE_URL, 'TEST_DATABASE_URL is required')
assert(process.env.MCP_TEST_BACKEND_BIN, 'MCP_TEST_BACKEND_BIN is required')
const database = new URL(process.env.TEST_DATABASE_URL)
assert(['postgres:', 'postgresql:'].includes(database.protocol))
assert.equal(database.hostname, '127.0.0.1')
assert.equal(database.pathname, '/frame_space_test')
const binary = resolve(backend, process.env.MCP_TEST_BACKEND_BIN)
assert((await stat(binary)).isFile())
const schema = `test_live_mcp_${Date.now()}_${randomBytes(4).toString('hex')}`
database.search = ''
database.searchParams.set('search_path', schema)
const children = [], browserErrors = [], checks = []
let browser, callbackURL

function sql(statement) {
  const result = spawnSync('wsl.exe', ['-d', 'Ubuntu-24.04', '-u', 'postgres', '--exec', 'psql', '-X', '-At', '-v', 'ON_ERROR_STOP=1', '-d', 'frame_space_test', '-c', statement], { encoding: 'utf8', windowsHide: true })
  assert.equal(result.status, 0, 'isolated PostgreSQL operation failed')
  return result.stdout.trim()
}
function launch(command, args, cwd, env) {
  const child = spawn(command, args, { cwd, env: { ...process.env, ...env }, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.on('data', () => {})
  child.stderr.on('data', () => {})
  children.push(child)
}
async function freePort() {
  const server = createServer()
  await new Promise((done, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', done) })
  const port = server.address().port
  await new Promise(done => server.close(done))
  return port
}
async function ready(url) {
  const until = Date.now() + 25000
  while (Date.now() < until) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return } catch {}
    await new Promise(done => setTimeout(done, 200))
  }
  throw new Error('local test service did not become ready')
}

const backendPort = await freePort()
let frontendPort = await freePort()
while (frontendPort === backendPort) frontendPort = await freePort()
const origin = `http://127.0.0.1:${frontendPort}`
const callback = createServer((request, response) => {
  const incoming = new URL(request.url, `http://127.0.0.1:${callback.address().port}`)
  if (incoming.pathname !== '/callback') { response.writeHead(204); response.end(); return }
  callbackURL = incoming
  response.setHeader('Content-Type', 'text/html; charset=utf-8')
  response.end('<h1>授权回调已收到</h1>')
})
await new Promise((done, fail) => { callback.once('error', fail); callback.listen(0, '127.0.0.1', done) })
const callbackOrigin = `http://127.0.0.1:${callback.address().port}`
sql(`CREATE SCHEMA ${schema} AUTHORIZATION frame_space`)
try {
  launch(binary, [], backend, { DATABASE_URL: database.toString(), PORT: String(backendPort), APP_ORIGINS: origin, MCP_PUBLIC_URL: origin + '/mcp', AUTH_COOKIE_SECURE: 'false', OSS_ENABLED: 'false', ARK_API_KEY: '', APIMART_API_KEY: '', GIN_MODE: 'release' })
  launch(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', String(frontendPort), '--strictPort'], frontend, { API_PROXY_TARGET: `http://127.0.0.1:${backendPort}` })
  await Promise.all([ready(origin + '/'), ready(`http://127.0.0.1:${backendPort}/health`)])

  const metadata = await (await fetch(origin + '/.well-known/oauth-authorization-server')).json()
  assert.equal(metadata.issuer, origin)
  const registration = await fetch(metadata.registration_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ client_name: 'MCP 浏览器验收应用', redirect_uris: [callbackOrigin + '/callback'], token_endpoint_auth_method: 'none', grant_types: ['authorization_code'] }) })
  assert.equal(registration.status, 201)
  const client = await registration.json()
  const verifier = randomBytes(32).toString('base64url')
  const authURL = new URL(metadata.authorization_endpoint)
  authURL.search = new URLSearchParams({ client_id: client.client_id, response_type: 'code', redirect_uri: callbackOrigin + '/callback', scope: 'canvases:read canvases:write generation:write', resource: origin + '/mcp', state: randomBytes(16).toString('hex'), code_challenge_method: 'S256', code_challenge: createHash('sha256').update(verifier).digest('base64url') }).toString()

  browser = await chromium.launch({ channel: 'chrome', headless: true })
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await context.newPage()
  page.on('pageerror', error => browserErrors.push(error.message))
  await page.goto(authURL.toString())
  await expect(page.getByRole('heading', { name: '进入你的工作空间' })).toBeVisible()
  await page.getByLabel('邮箱', { exact: true }).fill('mcp-canvas@example.test')
  await page.getByLabel('密码', { exact: true }).fill('mcp test password')
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权应用访问画布' })).toBeVisible()
  await expect(page.getByText('允许读取', { exact: true })).toBeVisible()
  await expect(page.getByText('允许生成', { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  checks.push('普通用户登录回跳与手机授权页面')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权回调已收到' })).toBeVisible()
  assert.equal(callbackURL.searchParams.get('error'), 'access_denied')
  assert.equal(sql(`SELECT count(*) FROM ${schema}.mcp_oauth_tokens`), '0')
  checks.push('取消授权不签发凭证')

  await page.goto(authURL.toString())
  await page.getByRole('button', { name: '授权访问画布', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权回调已收到' })).toBeVisible()
  assert.equal(callbackURL.searchParams.get('state'), authURL.searchParams.get('state'))
  assert.equal(callbackURL.searchParams.get('iss'), origin)
  const tokenResponse = await fetch(metadata.token_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams({ client_id: client.client_id, grant_type: 'authorization_code', code: callbackURL.searchParams.get('code'), redirect_uri: callbackOrigin + '/callback', code_verifier: verifier, resource: origin + '/mcp' }) })
  assert.equal(tokenResponse.status, 200)
  const token = await tokenResponse.json()
  const sessionResponse = await page.request.get(origin + '/api/auth/me')
  assert.equal(sessionResponse.status(), 200)
  const session = await sessionResponse.json()
  assert.equal(session.user.role, 'user')
  const canvasId = randomUUID()
  const createdCanvas = await page.request.post(origin + '/api/canvases', { headers: { Origin: origin, 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id }, data: { id: canvasId, title: 'MCP 浏览器验收' } })
  assert.equal(createdCanvas.status(), 201)
  const otherContext = await browser.newContext()
  const otherLogin = await otherContext.request.post(origin + '/api/auth/login', { headers: { Origin: origin, 'X-Requested-With': 'frame-space' }, data: { email: 'other-mcp@example.test', password: 'mcp test password' } })
  assert.equal(otherLogin.status(), 200)
  const otherSession = await otherLogin.json()
  const otherCanvasId = randomUUID()
  const otherCanvas = await otherContext.request.post(origin + '/api/canvases', { headers: { Origin: origin, 'X-CSRF-Token': otherSession.csrfToken, 'X-Session-User': otherSession.user.id }, data: { id: otherCanvasId, title: '其他用户的画布' } })
  assert.equal(otherCanvas.status(), 201)
  await otherContext.close()

  let requestID = 0
  async function rpc(method, params) {
    const response = await fetch(origin + '/mcp', { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': '2025-06-18', Authorization: 'Bearer ' + token.access_token }, body: JSON.stringify({ jsonrpc: '2.0', id: ++requestID, method, params }) })
    assert.equal(response.status, 200)
    return response.json()
  }
  const initialized = await rpc('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'browser-acceptance', version: '1.0.0' } })
  assert.equal(initialized.result.serverInfo.name, 'frame-space-canvas')
  const tools = await rpc('tools/list', {})
  assert.deepEqual(tools.result.tools.map(tool => tool.name).sort(), ['configure_canvas_node', 'create_canvas_node', 'get_canvas', 'get_generation_task', 'list_canvases', 'list_generation_models', 'quote_generation', 'start_generation'])
  const nodeId = randomUUID()
  const input = { canvasId, kind: 'image', nodeId, position: { x: 120, y: 150 } }
  const first = await rpc('tools/call', { name: 'create_canvas_node', arguments: input })
  assert.equal(first.result.structuredContent.nodeId, nodeId)
  assert.equal(first.result.structuredContent.created, true)
  const retry = await rpc('tools/call', { name: 'create_canvas_node', arguments: input })
  assert.equal(retry.result.structuredContent.created, false)
  const video = await rpc('tools/call', { name: 'create_canvas_node', arguments: { canvasId, kind: 'video' } })
  assert.equal(video.result.structuredContent.kind, 'video')
  checks.push('OAuth PKCE、工具发现、图片与视频节点创建和重试')

  const savedCanvas = await (await page.request.get(origin + '/api/canvases/' + canvasId)).json()
  savedCanvas.snapshot.nodes[0].data.prompt = '海边的小猫'
  savedCanvas.snapshot.nodes[1].data.referenceIds = [nodeId]
  const saved = await page.request.put(origin + '/api/canvases/' + canvasId, { headers: { Origin: origin, 'X-CSRF-Token': session.csrfToken, 'X-Session-User': session.user.id }, data: { version: savedCanvas.version, snapshot: savedCanvas.snapshot } })
  assert.equal(saved.status(), 200)
  const canvases = await rpc('tools/call', { name: 'list_canvases', arguments: {} })
  assert.deepEqual(canvases.result.structuredContent.canvases.map(canvas => canvas.canvasId), [canvasId])
  const firstPage = await rpc('tools/call', { name: 'get_canvas', arguments: { canvasId, limit: 1 } })
  assert.equal(firstPage.result.structuredContent.nodeCount, 2)
  assert.equal(firstPage.result.structuredContent.nodes[0].data.prompt, '海边的小猫')
  assert.equal(firstPage.result.structuredContent.nextOffset, 1)
  const secondPage = await rpc('tools/call', { name: 'get_canvas', arguments: { canvasId, limit: 1, offset: 1 } })
  assert.deepEqual(secondPage.result.structuredContent.nodes[0].data.referenceIds, [nodeId])
  const deniedCanvas = await rpc('tools/call', { name: 'get_canvas', arguments: { canvasId: otherCanvasId } })
  assert.equal(deniedCanvas.result.isError, true)
  checks.push('读取本人画布列表、分页节点与引用，拒绝访问其他用户画布')

  const models = await rpc('tools/call', { name: 'list_generation_models', arguments: { kind: 'image' } })
  assert(models.result.structuredContent.models.some(model => model.provider === 'apimart' && model.modelId === 'seedream-5-0-lite'))
  const configured = await rpc('tools/call', { name: 'configure_canvas_node', arguments: { canvasId, nodeId, expectedVersion: firstPage.result.structuredContent.version, patch: { model: { provider: 'apimart', modelId: 'seedream-5-0-lite', parameters: { size: '1:1', resolution: '2K', n: 1 } } } } })
  assert.equal(configured.result.structuredContent.node.data.modelSelection.modelId, 'seedream-5-0-lite')
  const quote = await rpc('tools/call', { name: 'quote_generation', arguments: { canvasId, nodeId } })
  assert(quote.result.structuredContent.points > 0)
  assert.equal(quote.result.structuredContent.quoteHash.length, 64)
  checks.push('读取模型、配置节点并报价，不提交生成任务')

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(origin + '/?canvas=' + canvasId)
  await expect(page.locator('.media-node')).toHaveCount(2)
  await expect(page.locator('.media-node').filter({ hasText: '图片节点 1' })).toHaveCount(1)
  await expect(page.locator('.media-node').filter({ hasText: '视频节点 2' })).toHaveCount(1)
  await page.reload()
  await expect(page.locator('.media-node')).toHaveCount(2)
  checks.push('真实画布恢复两个空节点并在刷新后保留')

  await page.goto(origin + '/oauth/connections')
  await expect(page.getByRole('heading', { name: '已授权的 AI 应用' })).toBeVisible()
  await page.getByRole('button', { name: '撤销授权', exact: true }).click()
  await expect(page.getByText('目前没有有效的应用授权。')).toBeVisible()
  const rejected = await fetch(origin + '/mcp', { method: 'POST', headers: { Authorization: 'Bearer ' + token.access_token } })
  assert.equal(rejected.status, 401)
  assert.equal(sql(`SELECT count(*) FROM ${schema}.generation_tasks`), '0')
  assert.equal(sql(`SELECT count(*) FROM ${schema}.assets`), '0')
  assert.deepEqual(browserErrors, [])
  checks.push('用户撤销授权后令牌失效，没有生成任务或素材')
  console.log(JSON.stringify({ passed: true, checks, providerCalls: 0 }))
} finally {
  await browser?.close()
  for (const child of children) child.kill()
  await Promise.all(children.map(child => child.exitCode !== null ? Promise.resolve() : new Promise(done => { child.once('exit', done); setTimeout(done, 3000) })))
  await new Promise(done => callback.close(done))
  sql(`DROP SCHEMA ${schema} CASCADE`)
}
