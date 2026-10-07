<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { auth, logout } from '../auth/session'
import { credits, readCreditLedger, refreshCredits, type CreditLedgerEntry } from '../api/credits'
const props = defineProps<{ beforeLogout?: () => Promise<boolean> }>()
const busy = ref(false)
const error = ref('')
const root = ref<HTMLElement | null>(null)
const ledgerOpen = ref(false)
const entries = ref<CreditLedgerEntry[]>([])
const hasMore = ref(false)
const ledgerLoading = ref(false)
const ledgerError = ref('')
let ledgerRevision = 0
const labels: Record<string, string> = { grant: '测试积分发放', reserve: '生成冻结', settle: '生成扣除', release: '生成退回', adjust: '积分调整' }
const signed = (value: number) => value > 0 ? `+${value}` : String(value)
const date = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
async function loadLedger(append = false) {
  if (ledgerLoading.value) return
  const revision = ++ledgerRevision
  ledgerLoading.value = true
  ledgerError.value = ''
  try {
    const result = await readCreditLedger(append ? entries.value.length : 0)
    if (revision !== ledgerRevision) return
    entries.value = append ? [...entries.value, ...result.entries.filter((entry) => !entries.value.some((old) => old.id === entry.id))] : result.entries
    hasMore.value = result.hasMore
  } catch (cause) {
    if (revision === ledgerRevision) ledgerError.value = cause instanceof Error ? cause.message : '积分记录暂时无法读取。'
  } finally { if (revision === ledgerRevision) ledgerLoading.value = false }
}
function toggleLedger() {
  ledgerOpen.value = !ledgerOpen.value
  if (ledgerOpen.value) { void refreshCredits(); void loadLedger() }
}
function closeOutside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) ledgerOpen.value = false
}
onMounted(() => document.addEventListener('pointerdown', closeOutside, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', closeOutside, true))
watch(() => [auth.status, auth.user?.id] as const, () => {
  ledgerRevision++
  ledgerOpen.value = false
  entries.value = []
  ledgerLoading.value = false
  ledgerError.value = ''
})
async function leave() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { if (props.beforeLogout && !await props.beforeLogout()) return; await logout() }
  catch (cause) { error.value = cause instanceof Error ? cause.message : '退出失败，请重试。' }
  finally { busy.value = false }
}
</script>
<template>
  <div ref="root" class="account-control">
    <span class="account-email" :title="auth.user?.email">{{ auth.user?.email }}</span>
    <button class="credit-pill" type="button" :aria-expanded="ledgerOpen" aria-label="查看积分余额和流水" @click="toggleLedger">积分 {{ credits.status === 'ready' ? credits.available : '…' }}</button>
    <button class="quiet-button" type="button" :disabled="busy" @click="leave">{{ busy ? '正在退出…' : '退出登录' }}</button>
    <p v-if="error" class="account-error" role="alert">{{ error }}</p>
    <section v-if="ledgerOpen" class="credit-ledger" aria-label="积分账户" @pointerdown.stop @wheel.stop>
      <header><h2>测试积分</h2><button type="button" aria-label="关闭积分记录" @click="ledgerOpen = false">×</button></header>
      <p v-if="credits.status === 'ready'" class="credit-balance">可用 <strong>{{ credits.available }}</strong><span>冻结 {{ credits.reserved }}</span></p>
      <p v-else-if="credits.error" class="credit-ledger-error" role="alert">{{ credits.error }}</p>
      <p v-else class="credit-ledger-status">正在读取余额…</p>
      <div class="ledger-heading"><h3>积分流水</h3><button type="button" :disabled="ledgerLoading" @click="loadLedger()">刷新</button></div>
      <p v-if="ledgerError" class="credit-ledger-error" role="alert">{{ ledgerError }}</p>
      <p v-if="!entries.length" class="credit-ledger-status">{{ ledgerLoading ? '正在读取…' : '暂无积分记录' }}</p>
      <ol v-else>
        <li v-for="entry in entries" :key="entry.id">
          <div><strong>{{ labels[entry.operation] || entry.operation }}</strong><time :datetime="entry.createdAt">{{ date(entry.createdAt) }}</time></div>
          <p>可用 {{ signed(entry.availableDelta) }} · 冻结 {{ signed(entry.reservedDelta) }}</p>
          <small>结余 {{ entry.availableAfter }} · 冻结 {{ entry.reservedAfter }}</small>
        </li>
      </ol>
      <button v-if="hasMore" class="ledger-more" type="button" :disabled="ledgerLoading" @click="loadLedger(true)">加载更多</button>
    </section>
  </div>
</template>

<style scoped>
.account-control > button { border: 1px solid #454b55; border-radius: 7px; background: #262a31; color: #d2d8e2; padding: 7px 10px; font-size: 12px; white-space: nowrap; cursor: pointer; }
.account-control > button:hover:not(:disabled) { background: #343b46; }
.account-control .credit-pill { border-color: #617694; background: #263446; color: #c9ddff; }
.credit-ledger { position: absolute; z-index: 60; top: calc(100% + 12px); right: 0; width: min(350px, calc(100vw - 32px)); max-height: min(520px, calc(100dvh - 110px)); overflow-y: auto; padding: 16px; border: 1px solid #4a5361; border-radius: 12px; background: #222830; color: #ecf0f7; box-shadow: 0 18px 46px #0009; user-select: text; }
.credit-ledger header,.ledger-heading,.credit-ledger li > div { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.credit-ledger h2,.credit-ledger h3 { margin: 0; font-size: 14px; font-weight: 600; }
.credit-ledger button { border: 1px solid #5a6573; border-radius: 6px; background: #333d49; color: #e6edf6; padding: 4px 8px; cursor: pointer; }
.credit-balance { display: flex; align-items: baseline; gap: 8px; margin: 18px 0; color: #bec9d8; font-size: 13px; }
.credit-balance strong { color: #fff; font-size: 24px; font-variant-numeric: tabular-nums; }
.credit-balance span { margin-left: auto; }
.ledger-heading { padding-bottom: 7px; border-bottom: 1px solid #424b55; }
.credit-ledger ol { margin: 0; padding: 0; list-style: none; }
.credit-ledger li { padding: 11px 0; border-bottom: 1px solid #3b424c; font-size: 12px; }
.credit-ledger time,.credit-ledger small,.credit-ledger li p,.credit-ledger-status { color: #aeb9c6; font-size: 11px; }
.credit-ledger li p { margin: 6px 0; font-variant-numeric: tabular-nums; }
.credit-ledger-error { color: #ffb8aa; font-size: 12px; line-height: 1.5; }
.ledger-more { display: block; margin: 12px auto 0; }
</style>
