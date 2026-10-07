import { reactive, watch } from 'vue'
import { auth } from '../auth/session'
import { persistenceRequest, type TaskInput } from './persistence'

export interface CreditQuote { points: number; priceVersion: string }
export interface CreditLedgerEntry {
  id: number
  operation: string
  availableDelta: number
  reservedDelta: number
  availableAfter: number
  reservedAfter: number
  taskId?: string
  createdAt: string
}

export const credits = reactive({
  available: null as number | null,
  reserved: null as number | null,
  status: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
  error: '',
})

function validPoints(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

let revision = 0
export async function refreshCredits() {
  const current = ++revision
  const userId = auth.user?.id
  if (auth.status !== 'authenticated' || !userId) return
  credits.status = 'loading'
  credits.error = ''
  try {
    const result = await persistenceRequest<{ available: number; reserved: number }>('/api/credits')
    if (current !== revision || auth.user?.id !== userId) return
    if (!validPoints(result.available) || !validPoints(result.reserved)) throw new Error('积分服务返回了无效余额。')
    credits.available = result.available
    credits.reserved = result.reserved
    credits.status = 'ready'
  } catch (cause) {
    if (current !== revision || auth.user?.id !== userId) return
    credits.status = 'error'
    credits.error = cause instanceof Error ? cause.message : '积分余额暂时无法读取。'
  }
}

export async function quoteGeneration(kind: 'image' | 'video', input: TaskInput, signal: AbortSignal): Promise<CreditQuote> {
  const result = await persistenceRequest<CreditQuote>('/api/credits/quote', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind, ...input }), signal,
  })
  if (!validPoints(result.points) || result.points === 0 || typeof result.priceVersion !== 'string' || !result.priceVersion) {
    throw new Error('积分服务返回了无效报价。')
  }
  return result
}

export async function readCreditLedger(offset = 0) {
  const result = await persistenceRequest<{ entries: CreditLedgerEntry[]; hasMore: boolean }>(`/api/credits/ledger?limit=20&offset=${offset}`)
  if (!Array.isArray(result.entries) || typeof result.hasMore !== 'boolean') throw new Error('积分流水返回了无效数据。')
  return result
}

watch(() => [auth.status, auth.user?.id] as const, ([status]) => {
  revision++
  credits.available = null
  credits.reserved = null
  credits.status = 'idle'
  credits.error = ''
  if (status === 'authenticated') void refreshCredits()
}, { immediate: true })
