import { test, expect } from './setup'
import type { Page } from '@playwright/test'

const user = { id: '88888888-8888-4888-8888-888888888888', email: 'creator@custom.example' }
const session = { user, csrfToken: 'mock-csrf', expiresAt: '2099-01-01T00:00:00Z' }
const password = 'a creative password'

async function authMock(page: Page, initial = false, acceptedPassword = password) {
  let signedIn = initial
  let rejectCanvasList = false
  const attempts: { email: string; password: string }[] = []
  const privateRequests: string[] = []
  const creates: string[] = []
  const canvases = new Map<string, unknown>()
  await page.route('**/api/auth/me', (route) => route.fulfill({ status: signedIn ? 200 : 401, json: signedIn ? session : { error: { code: 'AUTH_REQUIRED' } } }))
  await page.route('**/api/auth/login', (route) => {
    const body = route.request().postDataJSON()
    attempts.push(body)
    expect(route.request().headers()['x-requested-with']).toBe('frame-space')
    signedIn = body.email === user.email && body.password === acceptedPassword
    return route.fulfill({ status: signedIn ? 200 : 401, json: signedIn ? session : { error: { code: 'INVALID_CREDENTIALS', message: '邮箱或密码不正确，或账号不可用。' } } })
  })
  await page.route('**/api/auth/logout', (route) => {
    expect(route.request().headers()['x-csrf-token']).toBe(session.csrfToken)
    signedIn = false
    return route.fulfill({ status: 204 })
  })
  await page.route('**/api/persistence/config', (route) => {
    privateRequests.push('config')
    return route.fulfill({ json: { enabled: true } })
  })
  await page.route('**/api/credits', (route) => {
    privateRequests.push('credits')
    return route.fulfill({ json: { available: 200, reserved: 0 } })
  })
  await page.route('**/api/canvases**', (route) => {
    privateRequests.push('canvases')
    const request = route.request(), url = new URL(request.url())
    if (request.method() === 'POST') {
      const body = request.postDataJSON()
      creates.push(body.id)
      const canvas = { id: body.id, title: body.title, version: 0, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [], snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } } }
      canvases.set(body.id, canvas)
      return route.fulfill({ status: 201, json: canvas })
    }
    const id = url.pathname.split('/')[3]
    if (id) return route.fulfill({ json: canvases.get(id) ?? { id, title: '已有画布', version: 0, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [], snapshot: { nodes: [], viewport: { x: 0, y: 0, zoom: 1 } } } })
    if (rejectCanvasList) { signedIn = false; return route.fulfill({ status: 401, json: { error: { code: 'AUTH_REQUIRED', message: '登录已过期' } } }) }
    return route.fulfill({ json: { canvases: [], hasMore: false } })
  })
  return { attempts, privateRequests, creates, expire() { signedIn = false }, rejectList(value: boolean) { rejectCanvasList = value } }
}
async function fillLogin(page: Page) {
  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '继续', exact: true }).click()
}

test('访客首页可访问，登录后查看画布，退出后回到公开首页', async ({ page }) => {
  const mock = await authMock(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  expect(mock.privateRequests).toEqual([])
  await expect(page.getByRole('heading', { name: '我的画布' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toHaveCount(0)
  await page.getByRole('button', { name: '登录 / 注册' }).click()
  await expect(page.getByRole('heading', { name: '进入你的工作空间' })).toBeVisible()
  await expect(page.getByText('未注册的邮箱将自动创建账号。')).toBeVisible()
  await expect(page.getByRole('button', { name: /忘记密码/ })).toHaveCount(0)
  await fillLogin(page)
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  expect(mock.attempts).toHaveLength(1)
  await page.reload()
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  expect(mock.attempts).toHaveLength(1)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  await expect(page.getByText(user.email)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '全部画布' })).toHaveCount(0)
})

for (const value of ['123456', 'a'.repeat(20), '🔑'.repeat(20)]) {
  test(`密码边界可登录：${Array.from(value).length} 个字符、${value.length} 个编码单元`, async ({ page }) => {
    const mock = await authMock(page, false, value)
    await page.goto('/')
    await page.getByRole('button', { name: '登录 / 注册' }).click()
    await page.getByLabel('邮箱', { exact: true }).fill(user.email)
    await page.getByLabel('密码', { exact: true }).fill(value)
    await page.getByRole('button', { name: '继续', exact: true }).click()
    await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
    expect(mock.attempts).toEqual([{ email: user.email, password: value }])
  })
}

test('密码不足 6 个或超过 20 个字符时阻止提交，不截断输入', async ({ page }) => {
  const mock = await authMock(page)
  await page.goto('/')
  await page.getByRole('button', { name: '登录 / 注册' }).click()
  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  const input = page.getByLabel('密码', { exact: true })
  await expect(input).toHaveAttribute('placeholder', '6～20 个字符')
  for (const value of ['12345', 'a'.repeat(21), '🔑'.repeat(21), '      ']) {
    await input.fill(value)
    await page.getByRole('button', { name: '继续', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('密码需为 6～20 个字符')
    await expect(input).toHaveValue(value)
    expect(mock.attempts).toHaveLength(0)
  }
})

test('密码错误保持表单，允许显示密码，窄屏无横向溢出', async ({ page }) => {
  await authMock(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByRole('button', { name: '登录 / 注册' }).click()
  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  await page.getByLabel('密码', { exact: true }).fill('wrong password')
  await page.getByRole('button', { name: '显示密码', exact: true }).click()
  await expect(page.getByLabel('密码', { exact: true })).toHaveAttribute('type', 'text')
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('邮箱或密码不正确')
  await expect(page.getByLabel('邮箱', { exact: true })).toHaveValue(user.email)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: 'artifacts/auth-mobile.png', fullPage: true })
})

test('身份服务失败仍可浏览公开首页，并显示重试入口', async ({ page }) => {
  let failed = true
  await page.route('**/api/auth/me', (route) => route.fulfill({ status: failed ? 503 : 401, json: { error: { message: '登录服务暂时不可用，请稍后重试。' } } }))
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('登录服务暂时不可用')
  await expect(page.getByRole('button', { name: '重新连接' })).toBeVisible()
  await expect(page.getByLabel('邮箱', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '进入临时画布' })).toHaveCount(0)
  failed = false
  await page.getByRole('button', { name: '重新连接' }).click()
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  await page.getByRole('button', { name: '登录 / 注册' }).click()
  await expect(page.getByRole('heading', { name: '进入你的工作空间' })).toBeVisible()
})

test('访客点创建先登录，登录后打开命名框，确认才创建画布', async ({ page }) => {
  const mock = await authMock(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  expect(mock.privateRequests).toEqual([])
  await expect(page.getByText(user.email)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '全部画布' })).toHaveCount(0)
  await page.getByRole('button', { name: '创建画布', exact: true }).click()
  await expect(page.getByRole('heading', { name: '登录后创建画布' })).toBeVisible()
  expect(mock.creates).toEqual([])
  await fillLogin(page)
  await expect(page.getByRole('dialog', { name: '新建画布' })).toBeVisible()
  await expect(page.getByLabel('画布名称')).toBeFocused()
  expect(mock.creates).toEqual([])
  await page.getByLabel('画布名称').fill('访客创建的画布')
  await page.getByRole('dialog').getByRole('button', { name: '创建画布' }).click()
  await expect(page).toHaveURL(/\?canvas=/)
  expect(mock.creates).toHaveLength(1)
})

test('直达已有画布和临时画布仍要求先登录，匿名时不读取私有接口', async ({ page }) => {
  const mock = await authMock(page)
  const id = '77777777-7777-4777-8777-777777777777'
  for (const query of [`canvas=${id}`, `temporary=${id}`]) {
    mock.expire()
    mock.privateRequests.length = 0
    await page.goto(`/?${query}`)
    await expect(page.getByRole('heading', { name: '进入你的工作空间' })).toBeVisible()
    expect(mock.privateRequests).toEqual([])
    await fillLogin(page)
    await expect(page.getByRole('button', { name: '返回首页' })).toBeVisible()
    await expect(page).toHaveURL(new RegExp(query))
  }
})

test('保存时登录过期保留草稿，停止后台请求，同账号登录后保存', async ({ page }) => {
  const mock = await authMock(page, true)
  const canvasID = '77777777-7777-4777-8777-777777777777'
  const nodeID = '66666666-6666-4666-8666-666666666666'
  let rejectSave = true, saves = 0, polls = 0
  const canvas = { id: canvasID, title: '会话恢复', version: 0, updatedAt: new Date().toISOString(), assets: [], tasks: [], results: [], snapshot: { nodes: [{ id: nodeID, position: { x: 220, y: 220 }, data: { kind: 'image', name: '图片节点 1', prompt: '原草稿' } }], viewport: { x: 0, y: 0, zoom: 1 } } }
  await page.route('**/api/canvases/**', (route) => {
    if (route.request().url().includes('/tasks')) { polls++; return route.fulfill({ json: { tasks: [], hasMore: false } }) }
    if (route.request().method() === 'PUT') {
      if (rejectSave) { mock.expire(); return route.fulfill({ status: 401, json: { error: { code: 'AUTH_REQUIRED', message: '登录已过期' } } }) }
      const body = route.request().postDataJSON()
      expect(route.request().headers()['x-csrf-token']).toBe(session.csrfToken)
      expect(route.request().headers()['x-session-user']).toBe(user.id)
      saves++; canvas.snapshot = body.snapshot; canvas.version++
      return route.fulfill({ json: { version: canvas.version } })
    }
    return route.fulfill({ json: canvas })
  })
  await page.goto(`/?canvas=${canvasID}`)
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await page.getByRole('textbox').fill('过期期间不能丢失的草稿')
  await expect(page.getByRole('heading', { name: '继续你的创作' })).toBeVisible()
  await expect(page.getByRole('article', { name: '图片节点 1', exact: true })).not.toBeVisible()
  await expect(page.getByLabel('邮箱', { exact: true })).toHaveAttribute('readonly', '')
  const paused = polls
  await page.waitForTimeout(2200)
  expect(polls).toBe(paused)
  rejectSave = false
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '重新登录并继续' }).click()
  await expect(page.getByLabel('画布保存状态')).toHaveText('画布已保存')
  await page.getByRole('article', { name: '图片节点 1', exact: true }).click({ position: { x: 60, y: 70 } })
  await expect(page.getByRole('textbox')).toHaveText('过期期间不能丢失的草稿')
  expect(saves).toBeGreaterThan(0)
  expect(canvas.snapshot.nodes[0]!.data.prompt).toBe('过期期间不能丢失的草稿')
})

test('其他页面改变登录状态时隐藏当前账号内容', async ({ page }) => {
  await authMock(page, true)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  await page.evaluate(() => { const channel = new BroadcastChannel('frame-space-auth'); channel.postMessage({ type: 'logout' }); channel.close() })
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  await expect(page.getByRole('heading', { name: '我的画布' })).not.toBeVisible()
  await expect(page.getByText(user.email)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toHaveCount(0)
})

test('首页请求返回 401 时卸载个人画布和积分', async ({ page }) => {
  const mock = await authMock(page, true)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
  mock.rejectList(true)
  await page.getByRole('button', { name: '刷新列表' }).click()
  await expect(page.getByRole('heading', { name: /让想法/ })).toBeVisible()
  await expect(page.getByRole('heading', { name: '我的画布' })).toHaveCount(0)
  await expect(page.getByText(user.email)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看积分余额和流水' })).toHaveCount(0)
})
