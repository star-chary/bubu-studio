import { test, expect } from '@playwright/test'

const session = { user: { id: '11111111-1111-4111-8111-111111111111', email: 'operator@example.test', role: 'admin' }, csrfToken: 'test-csrf', expiresAt: '2099-01-01T00:00:00Z' }

test('管理员会话恢复后返回本站授权页', async ({ page }) => {
  await page.route('**/api/**', route => route.fulfill({ json: { session, maxGrantPoints: 10000, maxReasonLength: 200, mcpEnabled: true } }))
  await page.route('**/oauth/authorize?**', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>授权读取用户</h1>' }))
  await page.goto('./?mcp_return=' + encodeURIComponent('/oauth/authorize?client_id=test&state=keep-this'))
  await expect(page.getByRole('heading', { name: '授权读取用户' })).toBeVisible()
  await expect(page).toHaveURL(/\/oauth\/authorize\?client_id=test&state=keep-this$/)
})

test('登录回跳拒绝外站、其他接口和脚本地址', async ({ page }) => {
  await page.route('**/api/**', async route => {
    if (new URL(route.request().url()).pathname === '/api/admin/me') return route.fulfill({ json: { session, maxGrantPoints: 10000, maxReasonLength: 200, mcpEnabled: true } })
    return route.fulfill({ json: { users: [], total: 0 } })
  })
  for (const target of ['https://attacker.example/oauth/authorize', '//attacker.example/oauth/authorize', '/api/admin/users', 'javascript:alert(1)']) {
    await page.goto('./?mcp_return=' + encodeURIComponent(target))
    await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'AI 应用授权' })).toHaveAttribute('href', '/oauth/connections')
    expect(new URL(page.url()).pathname).toBe('/admin/')
  }
})
