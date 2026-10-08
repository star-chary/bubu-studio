import { reactive } from 'vue'

export interface User { id: string; email: string; role: 'user' | 'admin' }
export interface AdminUser extends User { status: 'active' | 'disabled'; createdAt: string; available: number; reserved: number }
interface Session { user: User; csrfToken: string; expiresAt: string }
export interface LedgerEntry {
  id: number; operation: string; availableDelta: number; reservedDelta: number;
  availableAfter: number; reservedAfter: number; createdAt: string; source: string;
  actorId?: string; actorEmail?: string; reason: string; taskId?: string;
}
export interface PendingGrant { userId: string; email: string; points: number; reason: string; requestId: string }
export const auth = reactive({ status: 'loading' as 'loading' | 'ready' | 'anonymous' | 'forbidden' | 'error', user: null as User | null, csrfToken: '', message: '', maxGrantPoints: 10000, maxReasonLength: 200 })
let epoch = 0
let requests = new AbortController()
const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('frame-space-auth')
function reset(status: typeof auth.status, message = '') {
  epoch++; requests.abort(); requests = new AbortController()
  auth.user = null; auth.csrfToken = ''; auth.status = status; auth.message = message
}
channel?.addEventListener('message', () => reset('anonymous', '登录状态已在其他页面变化，请重新登录。'))

export class ApiError extends Error { constructor(message: string, public status = 0) { super(message) } }
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const current = epoch
  const headers = new Headers(init.headers)
  if (auth.user) headers.set('X-Session-User', auth.user.id)
  if (init.body) headers.set('Content-Type', 'application/json')
  if (auth.csrfToken && !['GET', 'HEAD'].includes(init.method || 'GET')) headers.set('X-CSRF-Token', auth.csrfToken)
  let response: Response
  try {
    response = await fetch(path, { ...init, headers, credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.any([requests.signal, AbortSignal.timeout(20000), ...(init.signal ? [init.signal] : [])]) })
  } catch { throw new ApiError('连接中断或请求超时，请检查网络后重试。') }
  if (epoch !== current) throw new ApiError('登录状态已变化，请重新登录。')
  const body = response.status === 204 ? null : await response.json().catch(() => null)
  if (epoch !== current) throw new ApiError('登录状态已变化，请重新登录。')
  if (!response.ok) {
    const message = body?.error?.message || '请求失败，请稍后重试。'
    if (response.status === 401 && !path.endsWith('/login')) reset('anonymous', auth.user ? '登录已过期，请重新登录。' : '')
    if (body?.error?.code === 'ADMIN_REQUIRED') reset('forbidden', message)
    if (['SESSION_CHANGED', 'CSRF_INVALID'].includes(body?.error?.code)) reset('anonymous', message)
    throw new ApiError(message, response.status)
  }
  if (body === null && response.status !== 204) throw new ApiError('服务返回异常，请重试。')
  return body as T
}
export async function restoreSession() {
  auth.status = 'loading'; auth.message = ''
  const current = epoch
  try {
    const value = await api<{ session: Session; maxGrantPoints: number; maxReasonLength: number }>('/api/admin/me')
    if (current !== epoch) return
    if (value.session.user.role !== 'admin') { reset('forbidden', '该账号没有后台管理权限。'); return }
    auth.user = value.session.user; auth.csrfToken = value.session.csrfToken
    auth.maxGrantPoints = value.maxGrantPoints; auth.maxReasonLength = value.maxReasonLength
    auth.status = 'ready'
  } catch (error) {
    if (current !== epoch) return
    reset('error', error instanceof Error ? error.message : '登录服务暂时不可用。')
  }
}
export async function login(email: string, password: string) {
  await api<Session>('/api/admin/auth/login', { method: 'POST', headers: { 'X-Requested-With': 'frame-space' }, body: JSON.stringify({ email, password }) })
  channel?.postMessage({ type: 'login' })
  await restoreSession()
}
export async function logout() {
  await api('/api/auth/logout', { method: 'POST' })
  reset('anonymous'); channel?.postMessage({ type: 'logout' })
}
export function pendingKey() { return 'bubu-admin-pending-grant:' + auth.user?.id }
export function readPending(): PendingGrant | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(pendingKey()) || 'null')
    return value && typeof value.userId === 'string' && typeof value.requestId === 'string' && Number.isSafeInteger(value.points) && typeof value.reason === 'string' ? value : null
  } catch { return null }
}
export function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short', hour12: false }).format(new Date(value)) }
export function number(value: number) { return new Intl.NumberFormat('zh-CN').format(value) }
