<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import CanvasPage from './views/CanvasPage.vue'
import GuestHomePage from './views/GuestHomePage.vue'
import HomePage from './views/HomePage.vue'
import LoginPage from './views/LoginPage.vue'
import { auth, restoreAuth } from './auth/session'

// URLs identify documents. Each navigation creates a fresh canvas session,
// including its Vue Flow store, upload queue, prompt editor and task poller.
const params = new URLSearchParams(location.search)
const canvasId = params.get('canvas')
const temporaryId = params.get('temporary')
const isCanvasRoute = canvasId !== null || temporaryId !== null
const loginIntent = ref<'login' | 'create' | null>(null)
watch(() => auth.status, (status) => {
  if (status === 'authenticated' && loginIntent.value === 'login') loginIntent.value = null
  if (status === 'expired' && !isCanvasRoute) loginIntent.value = null
})
function restorePage(event: PageTransitionEvent) {
  if (event.persisted) location.reload()
}
onMounted(() => { window.addEventListener('pageshow', restorePage); void restoreAuth() })
onBeforeUnmount(() => window.removeEventListener('pageshow', restorePage))
</script>

<template>
  <div v-if="auth.status === 'loading' || (isCanvasRoute && auth.status === 'error')" class="auth-status" role="status">
    <img src="/favicon.svg" alt="" width="42" height="42" />
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
  <HomePage v-else-if="auth.status === 'authenticated'" :auto-open-create="loginIntent === 'create'" @auto-open-handled="loginIntent = null" />
  <LoginPage v-else-if="loginIntent !== null" :create-after-login="loginIntent === 'create'" />
  <GuestHomePage v-else :service-error="auth.status === 'error' ? auth.error : ''" :session-notice="auth.status === 'expired' ? auth.error : ''" @login="loginIntent = 'login'" @create="loginIntent = 'create'" @retry="restoreAuth()" />
</template>
