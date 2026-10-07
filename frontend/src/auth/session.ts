import { reactive } from 'vue'

export interface User { id: string; email: string }
interface SessionResponse { user: User; csrfToken: string; expiresAt: string }
export const auth = reactive({ status: 'loading' as 'loading' | 'authenticated' | 'anonymous' | 'expired' | 'error', user: null as User | null, csrfToken: '', error: '' })
let revision = 0
let requests = new AbortController()
const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('frame-space-auth')
channel?.addEventListener('message', () => {
  if (auth.status === 'authenticated') expireSession('登录状态已在其他页面变化，请重新登录。')
})

export class AuthError extends Error {}
function invalidateRequests() { revision++; requests.abort(); requests = new AbortController() }
export function expireSession(message = '登录已过期。重新登录后可继续编辑，当前未保存内容暂留在本页。') {
  if (auth.status === 'expired') return
  invalidateRequests()
  auth.status = auth.user ? 'expired' : 'anonymous'
  auth.csrfToken = ''; auth.error = message
}
function acceptSession(value: SessionResponse) {
  if (!value?.user?.id || !value.user.email || typeof value.csrfToken !== 'string') throw new Error('登录服务返回了无效数据，请重试。')
  if (auth.user && auth.user.id !== value.user.id) {
    invalidateRequests()
    location.replace('/')
    throw new AuthError('账号已切换，正在重新加载。')
  }
  invalidateRequests()
  auth.user = value.user; auth.csrfToken = value.csrfToken; auth.error = ''; auth.status = 'authenticated'
}
export async function restoreAuth() {
  auth.status = 'loading'; auth.error = ''
  const currentRevision = revision
  try {
    const response = await fetch('/api/auth/me', { credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.timeout(15_000) })
    if (currentRevision !== revision) return
    if (response.status === 401) {
      invalidateRequests(); auth.user = null; auth.csrfToken = ''; auth.status = 'anonymous'
      return
    }
    const body = await response.json().catch(() => null)
    if (currentRevision !== revision) return
    if (!response.ok) throw new Error(body?.error?.message || '登录服务暂时不可用，请稍后重试。')
    if (!body) throw new Error('登录服务返回了无效数据，请重试。')
    acceptSession(body)
  } catch (error) {
    if (currentRevision !== revision) return
    invalidateRequests(); auth.user = null; auth.csrfToken = ''; auth.status = 'error'
    auth.error = error instanceof Error && !['AbortError', 'TimeoutError', 'TypeError'].includes(error.name) ? error.message : '登录服务暂时不可用，请稍后重试。'
  }
}
export async function login(email: string, password: string) {
  const response = await fetch('/api/auth/login', {
    method: 'POST', credentials: 'same-origin', cache: 'no-store',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'frame-space' },
    body: JSON.stringify({ email, password }), signal: AbortSignal.timeout(20_000),
  })
  const body = await response.json().catch(() => null)
  if (!response.ok) throw new Error(body?.error?.message || '登录失败，请稍后重试。')
  acceptSession(body); channel?.postMessage({ type: 'login' })
}
export async function authFetch(path: string, init: RequestInit = {}): Promise<Response> {
  if (auth.status !== 'authenticated' || !auth.user) throw new AuthError('请重新登录后继续操作。')
  const currentRevision = revision
  const headers = new Headers(init.headers)
  headers.set('X-Session-User', auth.user.id)
  if (!['GET', 'HEAD', 'OPTIONS'].includes((init.method || 'GET').toUpperCase())) headers.set('X-CSRF-Token', auth.csrfToken)
  const signals = [requests.signal]
  if (init.signal) signals.push(init.signal)
  const response = await fetch(path, { ...init, headers, credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.any(signals) })
  if (currentRevision !== revision) throw new AuthError('登录状态已变化，请重新操作。')
  if (response.status === 401) expireSession()
  else if (response.status === 403 || response.status === 409) {
    const body = await response.clone().json().catch(() => null)
    if (['SESSION_CHANGED', 'CSRF_INVALID'].includes(body?.error?.code)) expireSession(body.error.message)
  }
  return response
}
export async function logout() {
  const response = await authFetch('/api/auth/logout', { method: 'POST' })
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.error?.message || '退出失败，请稍后重试。')
  }
  invalidateRequests(); auth.user = null; auth.csrfToken = ''; auth.status = 'anonymous'
  channel?.postMessage({ type: 'logout' })
  location.replace('/')
}
