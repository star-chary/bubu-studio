import { test, expect } from './setup'

const session = { user: { id: '88888888-8888-4888-8888-888888888888', email: 'creator@example.test' }, csrfToken: 'test-csrf', expiresAt: '2099-01-01T00:00:00Z' }
const target = '/oauth/authorize?client_id=test&state=keep-this'

test('普通用户从 MCP 授权进入登录页，登录后返回授权请求', async ({ page }) => {
  await page.route('**/api/auth/me', route => route.fulfill({ status: 401, json: { error: { code: 'AUTH_REQUIRED' } } }))
  await page.route('**/api/auth/login', route => route.fulfill({ json: session }))
  await page.route('**/oauth/authorize?**', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>授权应用访问画布</h1>' }))
  await page.goto('/?mcp_return=' + encodeURIComponent(target))
  await expect(page.getByRole('heading', { name: '进入你的工作空间' })).toBeVisible()
  await page.getByLabel('邮箱', { exact: true }).fill(session.user.email)
  await page.getByLabel('密码', { exact: true }).fill('regular password')
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await expect(page.getByRole('heading', { name: '授权应用访问画布' })).toBeVisible()
  await expect(page).toHaveURL(/\/oauth\/authorize\?client_id=test&state=keep-this$/)
})

test('已登录用户直接回到本站授权页', async ({ page }) => {
  await page.route('**/oauth/authorize?**', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>授权应用访问画布</h1>' }))
  await page.goto('/?mcp_return=' + encodeURIComponent(target))
  await expect(page.getByRole('heading', { name: '授权应用访问画布' })).toBeVisible()
})

test('登录回跳拒绝外站、其他接口和带片段地址', async ({ page }) => {
  for (const value of ['https://attacker.example/oauth/authorize', '//attacker.example/oauth/authorize', '/api/canvases', 'javascript:alert(1)', '/oauth/authorize#fragment']) {
    await page.goto('/?mcp_return=' + encodeURIComponent(value))
    await expect(page.getByRole('heading', { name: '我的画布' })).toBeVisible()
    expect(new URL(page.url()).pathname).toBe('/')
  }
})
