import { test, expect } from '@playwright/test'

test('管理员新增提示词、多图上传失败后重试，并能编辑和删除', async ({ page }, info) => {
  const user = { id: '11111111-1111-4111-8111-111111111111', email: 'admin@example.test', role: 'admin' }
  const session = { user, csrfToken: 'test-csrf', expiresAt: '2099-01-01T00:00:00Z' }
  let saved: { id: string; title: string; content: string; images: { id: string; url: string }[]; createdAt: string; updatedAt: string } | undefined
  let uploads = 0
  const attempted: string[] = []
  await page.route('**/api/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname, method = request.method()
    if (path === '/api/admin/me') return route.fulfill({ json: { session, maxGrantPoints: 10000, maxReasonLength: 200 } })
    if (path === '/api/admin/users') return route.fulfill({ json: { users: [], total: 0 } })
    if (path.startsWith('/api/prompt-images/')) return route.fulfill({ path: '../frontend/tests/fixtures/sample-image.png', contentType: 'image/png' })
    if (method !== 'GET') {
      expect(request.headers()['x-csrf-token']).toBe('test-csrf')
      expect(request.headers()['x-session-user']).toBe(user.id)
    }
    if (path === '/api/admin/prompts' && method === 'GET') return route.fulfill({ json: { prompts: saved ? [saved] : [], hasMore: false } })
    if (path === '/api/admin/prompts' && method === 'POST') {
      saved = { ...request.postDataJSON(), images: [], createdAt: '2026-11-01T00:00:00Z', updatedAt: '2026-11-01T00:00:00Z' }
      return route.fulfill({ status: 201, json: { id: saved!.id } })
    }
    const imageMatch = path.match(/^\/api\/admin\/prompts\/([^/]+)\/images\/([^/]+)$/)
    if (imageMatch && method === 'PUT' && saved) {
      expect(request.headers()['content-type']).toBe('application/octet-stream')
      uploads++; attempted.push(imageMatch[2]!)
      if (uploads === 2) return route.abort('failed')
      const image = { id: imageMatch[2]!, url: `/api/prompt-images/${imageMatch[2]}` }
      saved.images.push(image)
      return route.fulfill({ status: 201, json: image })
    }
    if (imageMatch && method === 'DELETE' && saved) { saved.images = saved.images.filter(image => image.id !== imageMatch[2]); return route.fulfill({ status: 204 }) }
    if (saved && path === `/api/admin/prompts/${saved.id}` && method === 'PUT') { Object.assign(saved, request.postDataJSON()); return route.fulfill({ status: 204 }) }
    if (saved && path === `/api/admin/prompts/${saved.id}` && method === 'DELETE') { saved = undefined; return route.fulfill({ status: 204 }) }
    return route.abort()
  })
  await page.goto('./')
  await page.getByRole('link', { name: '提示词管理' }).click()
  await page.getByRole('button', { name: '新增提示词' }).click()
  await page.getByLabel('标题', { exact: true }).fill('雨夜街景')
  await page.getByLabel('提示词内容').fill('霓虹灯和雨夜街道')
  await page.locator('input[type=file]').setInputFiles(['../frontend/tests/fixtures/sample-image.png', '../frontend/tests/fixtures/sample-image.png'])
  await expect(page.getByText('待上传', { exact: true })).toHaveCount(2)
  await page.screenshot({ path: info.outputPath('prompt-admin-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.locator('.workspace').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('prompt-admin-mobile.png') })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.getByRole('button', { name: '保存提示词' }).click()
  await expect(page.getByRole('alert')).toContainText('仍有 1 张图片待上传')
  await page.getByRole('button', { name: '保存提示词' }).click()
  await expect(page.getByText('提示词已保存。')).toBeVisible()
  expect(attempted).toHaveLength(3)
  expect(attempted[1]).toBe(attempted[2])
  await expect(page.getByText('2 张')).toBeVisible()
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  page.on('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: '删除已上传图片' }).first().click()
  await page.getByLabel('标题', { exact: true }).fill('修改后的标题')
  await page.getByRole('button', { name: '保存提示词' }).click()
  await expect(page.getByText('修改后的标题')).toBeVisible()
  await expect(page.getByText('1 张')).toBeVisible()
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByText('提示词已删除。')).toBeVisible()
  await expect(page.getByText('还没有提示词')).toBeVisible()
})
