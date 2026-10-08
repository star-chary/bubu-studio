<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref } from 'vue'
import { api, ApiError, auth, formatDate, number, pendingKey, readPending } from './api'
import type { AdminUser, LedgerEntry, PendingGrant } from './api'

const props = defineProps<{ user: AdminUser; startGrant: boolean }>()
const emit = defineEmits<{ close: []; granted: [points: number, email: string] }>()
const dialog = ref<HTMLDialogElement>(), amountInput = ref<HTMLInputElement>()
const user = ref<AdminUser>({ ...props.user }), entries = ref<LedgerEntry[]>([]), ledgerPage = ref(0), hasMore = ref(false), loading = ref(true), ledgerError = ref('')
const points = ref(''), reason = ref(''), grantError = ref(''), success = ref(''), busy = ref(false), review = ref(false)
const pending = ref<PendingGrant | null>(readPending())
const ownPending = computed(() => pending.value?.userId === user.value.id ? pending.value : null)
// A disabled account cannot receive a new grant, but a previous action must
// remain queryable: the server can confirm a committed entry without adding again.
const grantBlocked = computed(() => !ownPending.value && (user.value.status !== 'active' || !!pending.value))
const quantity = computed(() => Number(points.value))
let version = 0
function close() { if (busy.value) return; dialog.value?.close(); emit('close') }
async function loadLedger() {
  const current = ++version; loading.value = true; ledgerError.value = ''
  try {
    const value = await api<{ user: AdminUser; entries: LedgerEntry[]; hasMore: boolean }>(`/api/admin/users/${user.value.id}/ledger?limit=10&offset=${ledgerPage.value * 10}`)
    if (current !== version) return
    user.value = value.user; entries.value = value.entries; hasMore.value = value.hasMore
  } catch (e) { if (version === current) ledgerError.value = e instanceof Error ? e.message : '积分记录加载失败。' }
  finally { if (version === current) loading.value = false }
}
function turn(delta: number) { ledgerPage.value += delta; void loadLedger() }
function prepare() {
  grantError.value = ''; success.value = ''
  if (busy.value || grantBlocked.value) return
  if (!Number.isSafeInteger(quantity.value) || quantity.value < 1 || quantity.value > auth.maxGrantPoints || !reason.value.trim() || [...reason.value.trim()].length > auth.maxReasonLength) {
    grantError.value = `请输入 1–${number(auth.maxGrantPoints)} 的整数积分，并填写 ${auth.maxReasonLength} 字以内的原因。`; return
  }
  review.value = true
}
async function submit() {
  if (busy.value || grantBlocked.value || (!review.value && !ownPending.value)) return
  const key = pendingKey()
  const action: PendingGrant = ownPending.value || { userId: user.value.id, email: user.value.email, points: quantity.value, reason: reason.value.trim(), requestId: crypto.randomUUID() }
  try { sessionStorage.setItem(key, JSON.stringify(action)) }
  catch { grantError.value = '浏览器无法保存发放记录，请允许本站使用会话存储后重试。'; return }
  pending.value = action; busy.value = true; grantError.value = ''; success.value = ''
  try {
    const value = await api<{ account: { available: number; reserved: number }; entry: LedgerEntry; replayed: boolean }>(`/api/admin/users/${action.userId}/credits`, { method: 'POST', body: JSON.stringify({ points: action.points, reason: action.reason, requestId: action.requestId }) })
    // Clear only after a confirmed response. A lost response can safely replay.
    try { sessionStorage.removeItem(key) } catch { /* Replaying this ID remains safe. */ }
    pending.value = null; review.value = false
    Object.assign(user.value, value.account)
    success.value = value.replayed ? `已确认：这笔 ${number(action.points)} 积分已发放，没有重复增加。` : `发放成功，已增加 ${number(action.points)} 测试积分。`
    points.value = ''; reason.value = ''; ledgerPage.value = 0
    emit('granted', action.points, action.email)
    void loadLedger()
  } catch (e) {
    // 401/403 and transport/server failures retain the action across re-login.
    const definitive = e instanceof ApiError && [400, 404, 409, 422].includes(e.status)
    if (definitive) { try { sessionStorage.removeItem(key) } catch {} pending.value = null; review.value = false }
    grantError.value = e instanceof Error ? e.message : '请求未完成。'
  } finally { busy.value = false }
}
function operation(entry: LedgerEntry) {
  if (entry.operation === 'grant') return ({ manual: '管理员发放', signup: '注册赠分', legacy: '历史发放' } as Record<string, string>)[entry.source] || '系统发放'
  return ({ reserve: '生成冻结', settle: '生成结算', release: '失败退回' } as Record<string, string>)[entry.operation] || entry.operation
}
function signed(value: number) { return value > 0 ? '+' + number(value) : number(value) }
onMounted(async () => { dialog.value?.showModal(); void loadLedger(); if (props.startGrant && !pending.value) { await nextTick(); amountInput.value?.focus() } })
onBeforeUnmount(() => { version++ })
</script>

<template>
  <dialog ref="dialog" class="user-drawer" aria-labelledby="detail-title" @cancel.prevent="close" @click="(event) => { if (event.target === dialog) close() }">
    <div class="drawer-content">
      <header class="drawer-header"><div><p class="eyebrow">用户详情</p><h2 id="detail-title">积分管理</h2></div><button class="icon-button" aria-label="关闭用户详情" :disabled="busy" @click="close">×</button></header>
      <div class="drawer-scroll">
        <section class="detail-identity"><span class="user-avatar large" aria-hidden="true">{{ user.email[0]?.toUpperCase() }}</span><div><h3>{{ user.email }}</h3><span class="user-id">{{ user.id }}</span><p class="muted">{{ user.role === 'admin' ? '管理员' : '普通用户' }} · {{ user.status === 'active' ? '账号正常' : '账号已停用' }}</p></div></section>
        <div class="balance-strip"><div><span>可用积分</span><strong>{{ number(user.available) }}<small>分</small></strong></div><div><span>冻结积分</span><strong>{{ number(user.reserved) }}<small>分</small></strong></div></div>
        <section class="grant-section" aria-labelledby="grant-heading"><h3 id="grant-heading">发放测试积分</h3>
          <p v-if="grantBlocked" class="notice">{{ user.status !== 'active' ? '该账号已停用，不能发放积分。' : '请先确认上一笔发放结果，再开始新的发放。' }}</p>
          <p v-if="success" class="notice success" role="status">{{ success }}</p>
          <p v-if="grantError" class="notice error" role="alert">{{ grantError }}</p>
          <div v-if="ownPending" class="confirm-grant"><span class="eyebrow">{{ busy ? '正在确认发放' : '发放结果待确认' }}</span><strong>+{{ number(ownPending.points) }} <small>测试积分</small></strong><p>{{ ownPending.email }}</p><p class="reason-preview">{{ ownPending.reason }}</p><p class="muted">重试会核对同一笔发放，不会重复增加积分。</p><button class="primary" :disabled="busy" @click="submit">{{ busy ? '正在确认…' : '确认结果 / 重试' }}</button></div>
          <div v-else-if="review" class="confirm-grant"><span class="eyebrow">请核对本次发放</span><strong>+{{ number(quantity) }} <small>测试积分</small></strong><p>{{ user.email }}</p><span class="user-id">{{ user.id }}</span><p class="reason-preview">{{ reason.trim() }}</p><div class="button-row"><button class="secondary" @click="review = false">返回修改</button><button class="primary" :disabled="busy || grantBlocked" @click="submit">确认发放</button></div></div>
          <form v-else class="grant-form" @submit.prevent="prepare"><label for="grant-points">增加积分</label><div class="amount-field"><input id="grant-points" ref="amountInput" v-model="points" type="number" inputmode="numeric" min="1" :max="auth.maxGrantPoints" step="1" placeholder="输入发放数量" required :disabled="grantBlocked" /><span>积分</span></div><p class="field-help">每次可发放 1–{{ number(auth.maxGrantPoints) }} 积分。</p><label for="grant-reason">发放原因</label><textarea id="grant-reason" v-model="reason" rows="2" :maxlength="auth.maxReasonLength" placeholder="例如：产品体验测试补充额度" required :disabled="grantBlocked"></textarea><button class="primary" :disabled="grantBlocked">核对并发放</button></form>
        </section>
        <section class="ledger-section" aria-labelledby="ledger-heading"><div class="section-title"><h3 id="ledger-heading">积分记录</h3><button class="text-button" :disabled="loading" @click="ledgerPage = 0; loadLedger()">刷新</button></div>
          <div v-if="ledgerError" class="notice error" role="alert">{{ ledgerError }}<button class="text-button" @click="loadLedger">重试</button></div>
          <p v-else-if="loading" class="muted" role="status">正在读取记录…</p>
          <p v-else-if="entries.length === 0" class="muted">暂无积分记录。</p>
          <ol v-else class="ledger-list"><li v-for="entry in entries" :key="entry.id"><div class="ledger-line"><span class="ledger-dot" :class="entry.operation" aria-hidden="true"></span><strong>{{ operation(entry) }}</strong><span class="ledger-amount" :class="{ positive: entry.availableDelta > 0 }">{{ entry.operation === 'settle' ? signed(entry.reservedDelta) : signed(entry.availableDelta) }}<small> 分</small></span></div><div class="ledger-body"><p v-if="entry.reason" class="reason-preview">{{ entry.reason }}</p><p v-if="entry.actorEmail" class="muted">操作人：{{ entry.actorEmail }}</p><p class="ledger-balances">可用 {{ number(entry.availableAfter) }}<span>·</span>冻结 {{ number(entry.reservedAfter) }}</p><p class="ledger-meta">{{ formatDate(entry.createdAt) }}<span>流水 #{{ entry.id }}</span></p></div></li></ol>
          <nav class="ledger-pagination" aria-label="积分记录分页"><button class="secondary" :disabled="loading || ledgerPage === 0" @click="turn(-1)">上一页</button><span>第 {{ ledgerPage + 1 }} 页</span><button class="secondary" :disabled="loading || !hasMore" @click="turn(1)">下一页</button></nav>
        </section>
      </div>
    </div>
  </dialog>
</template>
