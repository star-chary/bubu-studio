<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { api, formatDate, number, readPending } from './api'
import type { AdminUser, PendingGrant } from './api'
import UserDetails from './UserDetails.vue'

const users = ref<AdminUser[]>([]), total = ref(0), query = ref(''), appliedQuery = ref(''), page = ref(0), loading = ref(true), error = ref(''), notice = ref('')
const selected = ref<AdminUser | null>(null), startGrant = ref(false), pending = ref<PendingGrant | null>(readPending())
const pageSize = 20
let version = 0
async function load() {
  const current = ++version; loading.value = true; error.value = ''
  try {
    const value = await api<{ users: AdminUser[]; total: number }>(`/api/admin/users?q=${encodeURIComponent(appliedQuery.value)}&limit=${pageSize}&offset=${page.value * pageSize}`)
    if (version !== current) return
    users.value = value.users; total.value = value.total
  } catch (e) { if (version === current) error.value = e instanceof Error ? e.message : '用户列表加载失败。' }
  finally { if (version === current) loading.value = false }
}
function search() { appliedQuery.value = query.value.trim(); page.value = 0; void load() }
function turn(amount: number) { page.value += amount; void load() }
function open(user: AdminUser, grant = false) { selected.value = user; startGrant.value = grant; notice.value = '' }
async function resume() {
  if (!pending.value) return
  try { const value = await api<{ user: AdminUser }>(`/api/admin/users/${pending.value.userId}/ledger?limit=1`); open(value.user, true) }
  catch (e) { error.value = e instanceof Error ? e.message : '无法加载待确认用户。' }
}
function closed() { selected.value = null; pending.value = readPending() }
function granted(points: number, email: string) { notice.value = `已向 ${email} 发放 ${number(points)} 测试积分。`; pending.value = readPending(); void load() }
onMounted(load)
onBeforeUnmount(() => { version++ })
</script>

<template>
  <main class="users-page">
    <div class="page-heading"><div><p class="eyebrow">用户与积分</p><h1>用户管理</h1><p class="muted">查看账号与积分余额，为用户发放测试积分。</p></div><span class="section-badge">测试积分</span></div>
    <p v-if="notice" class="notice success" role="status">{{ notice }}</p>
    <div v-if="pending" class="notice pending" role="status"><span>有一笔发放结果待确认：{{ pending.email }}，{{ number(pending.points) }} 积分。</span><button class="text-button" @click="resume">查看并重试</button></div>
    <section class="user-card" aria-label="用户列表">
      <div class="table-toolbar"><div class="table-title">全部用户 <span v-if="!loading" class="count">{{ number(total) }}</span></div><form class="search-form" @submit.prevent="search"><label class="sr-only" for="user-search">搜索邮箱或用户 ID</label><input id="user-search" v-model="query" placeholder="搜索邮箱或完整用户 ID" maxlength="254" /><button class="secondary" :disabled="loading">搜索</button><button class="icon-button" type="button" aria-label="刷新用户列表" :disabled="loading" @click="load">↻</button></form></div>
      <div v-if="error" class="empty-state" role="alert"><strong>用户列表暂时不可用</strong><p>{{ error }}</p><button class="secondary" @click="load">重试</button></div>
      <div v-else-if="loading" class="empty-state" role="status"><span class="spinner"></span>正在读取用户…</div>
      <div v-else-if="users.length === 0" class="empty-state"><span class="empty-icon" aria-hidden="true">◎</span><strong>{{ appliedQuery ? '没有找到匹配的用户' : '暂时没有用户' }}</strong><p>{{ appliedQuery ? '试试其他邮箱，或输入完整的用户 ID。' : '用户注册后，会出现在这里。' }}</p></div>
      <div v-else class="table-scroll"><table><thead><tr><th>用户</th><th>注册时间</th><th>状态</th><th class="numeric">可用积分</th><th class="numeric">冻结积分</th><th class="actions-heading">操作</th></tr></thead><tbody><tr v-for="user in users" :key="user.id"><td><div class="user-identity"><span class="user-avatar" aria-hidden="true">{{ user.email[0]?.toUpperCase() }}</span><div><strong>{{ user.email }}</strong><span class="user-id">{{ user.id }}</span><span v-if="user.role === 'admin'" class="role-tag">管理员</span></div></div></td><td class="date-cell">{{ formatDate(user.createdAt) }}</td><td><span class="state-badge" :class="user.status"><i></i>{{ user.status === 'active' ? '正常' : '已停用' }}</span></td><td class="numeric balance">{{ number(user.available) }}</td><td class="numeric muted">{{ number(user.reserved) }}</td><td><div class="row-actions"><button class="text-button" @click="open(user)">积分记录</button><button class="grant-button" :disabled="user.status !== 'active' || !!pending" @click="open(user, true)"><span aria-hidden="true">＋</span>发放积分</button></div></td></tr></tbody></table></div>
      <footer class="table-footer"><span>{{ appliedQuery ? '匹配用户' : '共' }} {{ number(total) }} 位<span class="footer-note"> · 按注册时间排序</span></span><nav aria-label="用户分页"><button class="secondary" :disabled="loading || page === 0" @click="turn(-1)">上一页</button><span>第 {{ page + 1 }} 页</span><button class="secondary" :disabled="loading || (page + 1) * pageSize >= total" @click="turn(1)">下一页</button></nav></footer>
    </section>
    <p class="page-footnote">可用积分用于发起生成；冻结积分由进行中的生成任务占用。</p>
    <UserDetails v-if="selected" :user="selected" :start-grant="startGrant" @close="closed" @granted="granted" />
  </main>
</template>
