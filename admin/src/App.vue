<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { auth, login, logout, restoreSession } from './api'
import UsersPage from './UsersPage.vue'

const email = ref(''), password = ref(''), visible = ref(false), busy = ref(false), error = ref('')
async function signIn() {
  busy.value = true; error.value = ''
  try { await login(email.value.trim(), password.value); password.value = '' }
  catch (e) { error.value = e instanceof Error ? e.message : '登录失败，请重试。' }
  finally { busy.value = false }
}
async function signOut() {
  busy.value = true; error.value = ''
  try { await logout() } catch (e) { error.value = e instanceof Error ? e.message : '退出失败，请重试。' }
  finally { busy.value = false }
}
function pageShow(event: PageTransitionEvent) { if (event.persisted) void restoreSession() }
onMounted(() => { void restoreSession(); window.addEventListener('pageshow', pageShow) })
onBeforeUnmount(() => window.removeEventListener('pageshow', pageShow))
</script>

<template>
  <div v-if="auth.status === 'loading'" class="loading-screen" role="status"><span class="brand-mark">B</span>正在连接管理台…</div>
  <div v-else-if="auth.status === 'ready'" class="workspace">
    <aside class="sidebar">
      <a class="brand" href="/admin/" aria-label="BuBu-后台管理首页"><span class="brand-mark">B</span><span>BuBu<small>后台管理</small></span></a>
      <div class="nav-caption">管理工作台</div>
      <a href="/admin/" class="nav-item" aria-current="page"><span aria-hidden="true">◎</span>用户管理</a>
      <div class="sidebar-bottom"><span class="status-dot"></span>测试积分阶段<small>BuBu-后台管理</small></div>
    </aside>
    <div class="workspace-body">
      <header class="topbar"><span>管理工作台 <span class="slash">/</span> 用户管理</span><div class="account"><span class="avatar" aria-hidden="true">{{ auth.user?.email[0]?.toUpperCase() }}</span><span class="account-email">{{ auth.user?.email }}</span><button class="text-button" :disabled="busy" @click="signOut">退出登录</button></div></header>
      <p v-if="error" class="notice error global-error" role="alert">{{ error }}</p>
      <UsersPage />
    </div>
  </div>
  <main v-else class="login-layout">
    <section class="login-brand"><a class="brand" href="/admin/"><span class="brand-mark">B</span><span>BuBu<small>后台管理</small></span></a><div class="login-statement"><p class="eyebrow">BuBu-后台管理</p><h1>让每一份创意，<br>都有继续的空间。</h1><p>查看用户、管理测试积分，<br>让每一次发放都有迹可循。</p><div class="brand-ledger" aria-hidden="true"><span>BuBu</span><div><i></i><i></i><i></i><i></i><i></i></div><small>创意，从这里继续。</small></div></div><span class="login-brand-footer">用户与积分管理</span></section>
    <section class="login-panel"><div class="login-form-wrap"><p class="eyebrow">管理员入口</p><h2>登录管理台</h2><p class="muted">使用已获授权的管理员账号继续。</p>
      <p v-if="auth.message" class="notice" :class="{ error: auth.status === 'error' }" role="status">{{ auth.message }}</p>
      <button v-if="auth.status === 'error'" class="secondary" @click="restoreSession">重新连接</button>
      <form @submit.prevent="signIn">
        <label for="email">邮箱</label><input id="email" v-model="email" type="email" autocomplete="username" maxlength="254" placeholder="请输入管理员邮箱" required :disabled="busy" />
        <label for="password">密码</label><div class="password-field"><input id="password" v-model="password" :type="visible ? 'text' : 'password'" autocomplete="current-password" placeholder="请输入密码" required :disabled="busy" /><button type="button" class="text-button" :aria-label="visible ? '隐藏密码' : '显示密码'" :aria-pressed="visible" @click="visible = !visible">{{ visible ? '隐藏' : '显示' }}</button></div>
        <p v-if="error" class="notice error" role="alert">{{ error }}</p>
        <button class="primary login-submit" :disabled="busy">{{ busy ? '正在登录…' : '登录管理台' }}<span aria-hidden="true">→</span></button>
      </form><p class="login-note">此入口仅供管理员使用。需要开通权限，请联系平台负责人。</p>
    </div><p class="login-footer">BuBu-后台管理 · 用户与积分</p></section>
  </main>
</template>
