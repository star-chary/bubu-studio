<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import CanvasPage from './views/CanvasPage.vue'
import HomeShell from './views/HomeShell.vue'
import LoginPage from './views/LoginPage.vue'
import { auth, restoreAuth } from './auth/session'

// URLs identify documents. Each navigation creates a fresh canvas session,
// including its Vue Flow store, upload queue, prompt editor and task poller.
const params = new URLSearchParams(location.search)
const canvasId = params.get('canvas')
const temporaryId = params.get('temporary')
const isCanvasRoute = canvasId !== null || temporaryId !== null
// OAuth may return only to the two same-origin pages owned by this app.
function oauthReturnTarget(value: string | null): string | null {
  if (!value || isCanvasRoute) return null
  try {
    const target = new URL(value, location.origin)
    if (target.origin === location.origin && !target.username && !target.password && !target.hash && ['/oauth/authorize', '/oauth/connections'].includes(target.pathname)) return target.href
  } catch { /* Ignore invalid or external return targets. */ }
  return null
}
const oauthReturn = oauthReturnTarget(params.get('mcp_return'))
const loginRequested = ref(!!oauthReturn)
watch(() => auth.status, (status) => {
  if (status === 'authenticated' && oauthReturn) { location.replace(oauthReturn); return }
  if (status === 'authenticated' || (status === 'expired' && !isCanvasRoute)) loginRequested.value = false
})
function restorePage(event: PageTransitionEvent) {
  if (event.persisted) location.reload()
}
onMounted(() => { window.addEventListener('pageshow', restorePage); void restoreAuth() })
onBeforeUnmount(() => window.removeEventListener('pageshow', restorePage))
</script>

<template>
  <div v-if="auth.status === 'loading' || ((isCanvasRoute || oauthReturn) && auth.status === 'error')" class="auth-status" role="status">
    <img src="/favicon.svg?v=bubu-2" alt="" width="42" height="42" />
    <p>{{ auth.status === 'loading' ? '正在连接工作空间…' : auth.error }}</p>
    <button v-if="auth.status === 'error'" class="secondary-button" @click="restoreAuth()">重新连接</button>
    <a v-if="auth.status === 'error'" href="/">返回首页</a>
  </div>
  <template v-else-if="isCanvasRoute">
    <LoginPage v-if="auth.status === 'anonymous'" />
    <div v-if="auth.status === 'authenticated' || auth.status === 'expired'" v-show="auth.status === 'authenticated'" class="authenticated-app" :inert="auth.status !== 'authenticated'">
      <CanvasPage :canvas-id="canvasId ?? temporaryId!" :temporary="canvasId === null" />
    </div>
    <LoginPage v-if="auth.status === 'expired'" :resume-email="auth.user?.email" />
  </template>
  <LoginPage v-else-if="loginRequested && auth.status !== 'authenticated'" />
  <HomeShell v-else :authenticated="auth.status === 'authenticated'" :service-error="auth.status === 'error' ? auth.error : ''" :session-notice="auth.status === 'expired' ? auth.error : ''" @login="loginRequested = true" @retry="restoreAuth()" />
</template>
