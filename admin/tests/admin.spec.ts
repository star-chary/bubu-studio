import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'

const operator = { id: '11111111-1111-4111-8111-111111111111', email: 'operator@example.test', role: 'admin' }
const target = { id: '22222222-2222-4222-8222-222222222222', email: 'creator@example.test', role: 'user', status: 'active', createdAt: '2026-10-08T02:00:00Z', available: 200, reserved: 12 }
const session = { user: operator, csrfToken: 'test-csrf', expiresAt: '2099-01-01T00:00:00Z' }
async function mock(page: Page, options: { signedIn?: boolean; lostResponse?: boolean } = {}) {
  let signedIn = options.signedIn ?? true, available = 200, attempts = 0, forbidden = false, disabled = false
  const requests: any[] = [], ledger: any[] = []
  const seen = new Map<string, any>()
  // A catch-all guarantees that regression tests cannot reach any real API.
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    if (path === '/api/admin/auth/login') {
      expect(route.request().headers()['x-requested-with']).toBe('frame-space')
      const body = route.request().postDataJSON(); signedIn = body.email === operator.email && body.password === 'test password'
      return route.fulfill({ status: signedIn ? 200 : 401, json: signedIn ? session : { error: { message: '邮箱或密码不正确，或账号不可用。' } } })
    }
    if (!signedIn) return route.fulfill({ status: 401, json: { error: { code: 'AUTH_REQUIRED' } } })
    if (path === '/api/auth/logout') { expect(route.request().headers()['x-csrf-token']).toBe('test-csrf'); signedIn = false; return route.fulfill({ status: 204 }) }
    if (forbidden) return route.fulfill({ status: 403, json: { error: { code: 'ADMIN_REQUIRED', message: '该账号没有后台管理权限。' } } })
    if (path === '/api/admin/me') return route.fulfill({ json: { session, maxGrantPoints: 10000, maxReasonLength: 200 } })
    if (path === '/api/admin/users') {
      const q = url.searchParams.get('q') || '', offset = Number(url.searchParams.get('offset') || 0)
      const matches = !q || target.email.includes(q) || q === target.id
      return route.fulfill({ json: { users: matches && offset === 0 ? [{ ...target, available, status: disabled ? 'disabled' : 'active' }] : [], total: matches ? 1 : 0 } })
    }
    if (path === `/api/admin/users/${target.id}/ledger`) return route.fulfill({ json: { user: { ...target, available, status: disabled ? 'disabled' : 'active' }, entries: ledger, hasMore: false } })
    if (path === `/api/admin/users/${target.id}/credits`) {
      expect(route.request().headers()['x-csrf-token']).toBe('test-csrf')
      expect(route.request().headers()['x-session-user']).toBe(operator.id)
      const body = route.request().postDataJSON(); requests.push(body); attempts++
      let entry = seen.get(body.requestId)
      const replayed = !!entry
      if (!entry) {
        available += body.points
        entry = { id: attempts, operation: 'grant', availableDelta: body.points, reservedDelta: 0, availableAfter: available, reservedAfter: 12, createdAt: '2026-10-08T02:20:00Z', source: 'manual', actorId: operator.id, actorEmail: operator.email, reason: body.reason }
        seen.set(body.requestId, entry); ledger.unshift(entry)
      }
      if (options.lostResponse && attempts === 1) return route.abort('failed')
      return route.fulfill({ json: { account: { available, reserved: 12 }, entry, replayed } })
    }
    return route.abort()
  })
  return { requests, get available() { return available }, revoke: () => { forbidden = true }, expire: () => { signedIn = false }, disableTarget: () => { disabled = true } }
}
async function grant(page: Page, amount = '50') {
  await page.getByRole('button', { name: '发放积分', exact: true }).click()
  await page.getByLabel('增加积分', { exact: true }).fill(amount)
  await page.getByLabel('发放原因', { exact: true }).fill('测试体验补充')
  await page.getByRole('button', { name: '核对并发放' }).click()
  await expect(page.getByText('请核对本次发放', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '确认发放', exact: true }).click()
}

test('管理入口登录、刷新恢复和退出', async ({ page }) => {
  await mock(page, { signedIn: false }); await page.goto('./')
  await expect(page).toHaveTitle('BuBu-后台管理')
  await page.getByLabel('邮箱', { exact: true }).fill(operator.email)
  await page.getByLabel('密码', { exact: true }).fill('wrong password')
  await page.getByRole('button', { name: '登录管理台', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('邮箱或密码不正确')
  await page.getByLabel('密码', { exact: true }).fill('test password')
  await page.getByRole('button', { name: '登录管理台', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  await page.reload(); await expect(page.getByRole('table')).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录管理台', exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toHaveCount(0)
})

test('搜索空结果与积分发放、确认、流水', async ({ page }) => {
  const state = await mock(page); await page.goto('./')
  await page.getByLabel('搜索邮箱或用户 ID').fill('missing')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.getByText('没有找到匹配的用户')).toBeVisible()
  await page.getByLabel('搜索邮箱或用户 ID').fill('creator')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await grant(page)
  await expect(page.getByRole('dialog').getByRole('status').filter({ hasText: '发放成功' })).toBeVisible()
  await expect(page.getByRole('dialog').getByText('操作人：operator@example.test')).toBeVisible()
  expect(state.available).toBe(250); expect(state.requests).toHaveLength(1)
  await page.getByRole('button', { name: '关闭用户详情' }).click()
  await expect(page.getByRole('table')).toContainText('250')
})

test('响应丢失后刷新、恢复和重试使用同一个发放编号', async ({ page }) => {
  const state = await mock(page, { lostResponse: true }); await page.goto('./'); await grant(page)
  await expect(page.getByText('发放结果待确认', { exact: true })).toBeVisible()
  state.disableTarget()
  await page.reload()
  await page.getByRole('button', { name: '查看并重试' }).click()
  await page.getByRole('button', { name: '确认结果 / 重试' }).click()
  await expect(page.getByRole('dialog').getByRole('status').filter({ hasText: '没有重复增加' })).toBeVisible()
  expect(state.requests).toHaveLength(2)
  expect(state.requests[0].requestId).toBe(state.requests[1].requestId)
  expect(state.available).toBe(250)
  await page.getByRole('button', { name: '关闭用户详情' }).click()
  await expect(page.getByRole('button', { name: '发放积分', exact: true })).toBeDisabled()
})

test('权限撤销后清除管理界面', async ({ page }) => {
  const state = await mock(page); await page.goto('./'); await expect(page.getByRole('table')).toBeVisible()
  state.revoke(); await page.getByRole('button', { name: '刷新用户列表' }).click()
  await expect(page.getByText('该账号没有后台管理权限。')).toBeVisible()
  await expect(page.getByRole('table')).toHaveCount(0)
})

test('会话过期后重新登录且保留待确认发放', async ({ page }) => {
  const state = await mock(page, { lostResponse: true }); await page.goto('./'); await grant(page)
  await expect(page.getByText('发放结果待确认', { exact: true })).toBeVisible()
  state.expire(); await page.getByRole('button', { name: '确认结果 / 重试' }).click()
  await expect(page.getByRole('heading', { name: '登录管理台', exact: true })).toBeVisible()
  await page.getByLabel('邮箱', { exact: true }).fill(operator.email)
  await page.getByLabel('密码', { exact: true }).fill('test password')
  await page.getByRole('button', { name: '登录管理台', exact: true }).click()
  await page.getByRole('button', { name: '查看并重试' }).click()
  await page.getByRole('button', { name: '确认结果 / 重试' }).click()
  await expect(page.getByRole('dialog').getByRole('status').filter({ hasText: '没有重复增加' })).toBeVisible()
  expect(state.available).toBe(250)
})

test('手机宽度下可操作，弹层可用键盘退出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 }); await mock(page); await page.goto('./')
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: '积分记录', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  expect(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= innerWidth)).toBe(true)
  await page.keyboard.press('Escape'); await expect(page.getByRole('dialog')).toHaveCount(0)
})
