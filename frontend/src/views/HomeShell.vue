<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import AccountControl from '../components/AccountControl.vue'
import McpConnectDialog from '../components/McpConnectDialog.vue'
import HomePage from './HomePage.vue'
import PromptLibrary from './PromptLibrary.vue'

withDefaults(defineProps<{ authenticated?: boolean; serviceError?: string; sessionNotice?: string }>(), {
  authenticated: false, serviceError: '', sessionNotice: '',
})
const emit = defineEmits<{ login: []; retry: [] }>()
const viewFromURL = () => new URLSearchParams(location.search).get('view') === 'prompts' ? 'prompts' : 'home'
const view = ref<'home' | 'prompts'>(viewFromURL())
const showMcpConnect = shallowRef(false)
function select(next: 'home' | 'prompts') {
  if (view.value === next) return
  history.pushState(null, '', next === 'prompts' ? '/?view=prompts' : '/')
  view.value = next
}
function onPopState() { view.value = viewFromURL() }
onMounted(() => window.addEventListener('popstate', onPopState))
onBeforeUnmount(() => window.removeEventListener('popstate', onPopState))
</script>

<template>
  <div class="public-shell">
    <aside class="public-sidebar">
      <a class="brand" href="/" aria-label="Bubu 首页" @click.prevent="select('home')">
        <img src="/favicon.svg?v=bubu-2" alt="" width="32" height="32" /><span class="brand-name">Bubu</span>
      </a>
      <nav aria-label="主导航">
        <a href="/" :aria-current="view === 'home' ? 'page' : undefined" @click.prevent="select('home')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="m3 10 9-7 9 7v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M9 21v-7h6v7"/></svg>首页
        </a>
        <a href="/?view=prompts" :aria-current="view === 'prompts' ? 'page' : undefined" @click.prevent="select('prompts')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><rect x="4" y="3" width="16" height="18" rx="3"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>提示词
        </a>
        <button type="button" aria-haspopup="dialog" :aria-expanded="showMcpConnect" @click="showMcpConnect = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10 13a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1.7 1.7"/><path d="M14 11a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.7-1.7"/></svg>MCP 连接
        </button>
      </nav>
    </aside>
    <div class="public-workspace">
      <header class="public-header">
        <span>{{ view === 'home' ? '首页' : '提示词' }}</span>
        <AccountControl v-if="authenticated" />
        <button v-else class="sign-in-button" type="button" @click="emit('login')">登录 / 注册</button>
      </header>
      <div class="public-page">
        <HomePage v-if="view === 'home'" :authenticated="authenticated" :service-error="serviceError" :session-notice="sessionNotice" @login="emit('login')" @retry="emit('retry')" />
        <PromptLibrary v-else />
      </div>
    </div>
    <McpConnectDialog v-if="showMcpConnect" @close="showMcpConnect = false" />
  </div>
</template>

<style scoped>
.public-shell { display: flex; width: 100%; height: 100%; height: 100dvh; background: #212121; color: #ededed; }
.public-sidebar { width: 220px; flex: 0 0 220px; padding: 25px 14px; background: #171717; border-right: 1px solid #303030; }
.brand { padding: 0 10px; color: #f2f2f2; text-decoration: none; }
.public-sidebar nav { display: grid; gap: 5px; margin-top: 45px; }
.public-sidebar nav a, .public-sidebar nav button { display: flex; align-items: center; gap: 13px; height: 44px; padding: 0 13px; border: 0; border-radius: 9px; background: transparent; color: #c4c4c4; text-decoration: none; font-size: 14px; white-space: nowrap; cursor: pointer; }
.public-sidebar nav a:hover, .public-sidebar nav button:hover { background: #262626; }
.public-sidebar nav a[aria-current="page"] { color: #f5f5f5; background: #303030; font-weight: 600; }
.public-sidebar nav svg { width: 19px; height: 19px; flex: none; }
.public-workspace { display: flex; flex: 1; min-width: 0; min-height: 0; flex-direction: column; }
.public-header { flex: 0 0 64px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 36px; border-bottom: 1px solid #343434; color: #aaa; font-size: 13px; }
.public-header :deep(.account-control > button) { border-color: #454545; background: #2a2a2a; color: #e5e5e5; }
.public-header :deep(.account-control .credit-pill) { border-color: #454545; background: #2a2a2a; color: #e5e5e5; }
.sign-in-button { padding: 9px 14px; border: 1px solid #505050; border-radius: 9px; background: transparent; color: #ededed; font-size: 13px; cursor: pointer; }
.sign-in-button:hover { background: #303030; }
.public-page { flex: 1; min-height: 0; }
.public-sidebar a:focus-visible, .public-sidebar button:focus-visible, .sign-in-button:focus-visible { outline: 2px solid #ededed; outline-offset: 3px; }
@media (max-width: 700px) {
  .public-shell { flex-direction: column; }
  .public-sidebar { display: flex; align-items: center; justify-content: space-between; gap: 10px; width: auto; flex: 0 0 auto; padding: 12px 16px; border-right: 0; border-bottom: 1px solid #303030; }
  .public-sidebar nav { display: flex; gap: 4px; margin: 0; }
  .public-sidebar nav a, .public-sidebar nav button { gap: 7px; height: 38px; padding: 0 8px; font-size: 13px; }
  .public-sidebar nav svg { width: 17px; height: 17px; }
  .brand { padding: 0; }
  .public-header { flex-basis: 54px; padding: 0 18px; }
}
@media (max-width: 360px) {
  .public-sidebar { gap: 6px; padding-inline: 12px; }
  .public-sidebar .brand-name { display: none; }
  .public-sidebar nav a, .public-sidebar nav button { gap: 5px; padding-inline: 6px; }
}
</style>
