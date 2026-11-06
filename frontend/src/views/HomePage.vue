<script setup lang="ts">
import AccountControl from '../components/AccountControl.vue'
import { nextTick, onMounted, ref } from 'vue'
import { persistenceRequest, writeJSON, type CanvasSummary, type SavedCanvas } from '../api/persistence'

const props = withDefaults(defineProps<{ authenticated?: boolean; serviceError?: string; sessionNotice?: string }>(), {
  authenticated: false,
  serviceError: '',
  sessionNotice: '',
})
const emit = defineEmits<{ login: []; retry: [] }>()
const canvases = ref<CanvasSummary[]>([])
const loading = ref(true)
const enabled = ref(false)
const error = ref('')
const hasMore = ref(false)
const dialog = ref<HTMLDialogElement | null>(null)
const titleInput = ref<HTMLInputElement | null>(null)
const title = ref('')
const creating = ref(false)
const createError = ref('')
let creationId = ''
let offset = 0
const temporaryURL = `/?temporary=${crypto.randomUUID()}`

async function load(append = false) {
  loading.value = true
  error.value = ''
  try {
    if (!append) {
      const config = await persistenceRequest<{ enabled: boolean }>('/api/persistence/config')
      if (typeof config.enabled !== 'boolean') throw new Error('无法读取画布服务状态，请重试。')
      enabled.value = config.enabled
      if (!enabled.value) return
    }
    const result = await persistenceRequest<{ canvases: CanvasSummary[]; hasMore: boolean }>(`/api/canvases?limit=24&offset=${append ? offset : 0}`)
    if (append) {
      const ids = new Set(canvases.value.map((canvas) => canvas.id))
      canvases.value.push(...result.canvases.filter((canvas) => !ids.has(canvas.id)))
      offset += result.canvases.length
    } else { canvases.value = result.canvases; offset = result.canvases.length }
    hasMore.value = result.hasMore
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '画布列表加载失败，请重试。' }
  finally { loading.value = false }
}

async function openCreate() {
  if (!props.authenticated) { emit('login'); return }
  title.value = ''
  createError.value = ''
  creationId = crypto.randomUUID()
  dialog.value?.showModal()
  await nextTick()
  titleInput.value?.focus()
}
async function create() {
  if (creating.value) return
  creating.value = true
  createError.value = ''
  try {
    const canvas = await writeJSON<SavedCanvas>('/api/canvases', { id: creationId, title: title.value.trim() || '未命名画布' })
    location.assign(`/?canvas=${encodeURIComponent(canvas.id)}`)
  } catch (cause) {
    createError.value = cause instanceof Error ? cause.message : '创建失败，请重试。'
    creating.value = false
  }
}
function preview(canvas: CanvasSummary) {
  if (!canvas.preview.length) return []
  const minX = Math.min(...canvas.preview.map((node) => node.position.x))
  const minY = Math.min(...canvas.preview.map((node) => node.position.y))
  const width = Math.max(...canvas.preview.map((node) => node.position.x)) - minX + 260
  const height = Math.max(...canvas.preview.map((node) => node.position.y)) - minY + 190
  const scale = Math.min(240 / width, 110 / height, .36)
  return canvas.preview.map((node) => ({ ...node,
    x: (300 - width * scale) / 2 + (node.position.x - minX) * scale,
    y: (160 - height * scale) / 2 + (node.position.y - minY) * scale,
    width: 260 * scale, height: 190 * scale,
  }))
}
function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}
onMounted(() => { if (props.authenticated) void load() })
</script>

<template>
  <div class="home-page">
    <header class="home-header">
      <a class="brand" href="/" aria-label="帧间首页">
        <img class="brand-mark" src="/favicon.svg" alt="" width="34" height="34" />
        <span class="brand-name">帧间</span>
      </a>
      <AccountControl v-if="authenticated" />
      <button v-else class="sign-in-button" type="button" @click="emit('login')">登录 / 注册</button>
    </header>
    <main class="home-main">
      <div class="library-heading">
        <div><h1>我的画布</h1><p>从一个想法开始，或接着上次继续。</p></div>
        <button v-if="authenticated" class="primary-button" type="button" :disabled="!enabled || loading" @click="openCreate">
          <span aria-hidden="true">＋</span> 新建画布
        </button>
      </div>
      <div class="library-toolbar">
        <h2>全部画布</h2><div class="library-rule" aria-hidden="true"></div>
        <template v-if="authenticated">
          <span>按最近修改排序</span>
          <button class="quiet-button refresh-button" type="button" :disabled="loading" @click="load()">刷新列表</button>
        </template>
      </div>
      <div v-if="serviceError" class="library-notice" role="alert">
        <p>登录服务暂时不可用：{{ serviceError }}</p><button class="secondary-button" type="button" @click="emit('retry')">重新连接</button>
      </div>
      <p v-else-if="sessionNotice" class="session-notice" role="status">{{ sessionNotice }}</p>
      <div v-if="error" class="library-notice" role="alert">
        <p>{{ error }}</p><button class="secondary-button" type="button" :disabled="loading" @click="load()">重新加载</button>
      </div>
      <p v-if="authenticated && loading && !canvases.length" class="library-status" role="status">正在加载画布…</p>
      <div v-else-if="authenticated && !enabled && !error" class="library-notice">
        <h2>画布保存服务未启用</h2>
        <p>启动数据库和后端后，便可新建画布并在这里继续编辑。</p>
        <a :href="temporaryURL">进入临时画布</a><p class="temporary-hint">临时内容在离开或刷新后不会保留。</p>
      </div>
      <template v-if="!authenticated || enabled">
        <p v-if="authenticated && !loading && !error && !canvases.length" class="empty-library">还没有画布。新建一张，放入你的第一份素材。</p>
        <div class="canvas-grid">
          <div class="new-canvas-item">
            <button class="new-canvas-tile" type="button" aria-label="创建空白画布" :disabled="authenticated && loading" @click="openCreate">
              <span class="new-frame" aria-hidden="true">＋</span>
              <strong>开始创作</strong>
            </button>
            <span class="new-canvas-caption">创建空白画布</span>
          </div>
          <a v-for="canvas in authenticated ? canvases : []" :key="canvas.id" class="canvas-card" :href="`/?canvas=${canvas.id}`" :aria-label="`打开画布：${canvas.title}（${canvas.id.slice(0, 8)}）`">
            <div class="canvas-preview" aria-hidden="true">
              <svg v-if="canvas.preview.length" viewBox="0 0 300 160" fill="none">
                <g v-for="(node, index) in preview(canvas)" :key="index" :class="`preview-${node.kind}`">
                  <rect :x="node.x" :y="node.y" :width="node.width" :height="node.height" rx="3" />
                  <path :d="`M${node.x + node.width * .2} ${node.y + node.height * .75}h${node.width * .6}`" />
                  <text v-if="node.width > 32" :x="node.x + node.width / 2" :y="node.y + node.height * .48" text-anchor="middle">{{ node.kind === 'image' ? '图片' : node.kind === 'video' ? '视频' : '音频' }}</text>
                </g>
              </svg>
              <svg v-else class="empty-preview" viewBox="0 0 48 48" fill="currentColor"><circle cx="33" cy="14" r="4" /><path d="M4 37 18 17a2 2 0 0 1 3.3 0l7.2 10.2 3.3-4.6a2 2 0 0 1 3.2 0L44 37Z" /></svg>
            </div>
            <div class="canvas-card-info">
              <h3 :title="canvas.title">{{ canvas.title }}</h3>
              <div class="canvas-card-meta"><time :datetime="canvas.updatedAt">{{ formatDate(canvas.updatedAt) }}</time><span>{{ canvas.nodeCount ? `${canvas.nodeCount} 个节点` : '空白画布' }}</span></div>
            </div>
          </a>
        </div>
        <button v-if="authenticated && hasMore" class="secondary-button more-canvases" type="button" :disabled="loading" @click="load(true)">{{ loading ? '正在加载…' : '加载更多画布' }}</button>
      </template>
    </main>
    <dialog ref="dialog" class="create-dialog" aria-labelledby="create-title" @cancel="creating && $event.preventDefault()">
      <form @submit.prevent="create">
        <div class="dialog-heading"><h2 id="create-title">新建画布</h2><button class="quiet-button" type="button" aria-label="关闭新建画布" :disabled="creating" @click="dialog?.close()">×</button></div>
        <p>给这次创作起个名字，方便下次找到。</p>
        <label for="canvas-title">画布名称</label>
        <input id="canvas-title" ref="titleInput" v-model="title" maxlength="80" placeholder="未命名画布" autocomplete="off" :disabled="creating" />
        <p v-if="createError" class="create-error" role="alert">{{ createError }}</p>
        <div class="dialog-actions"><button class="secondary-button" type="button" :disabled="creating" @click="dialog?.close()">取消</button><button class="primary-button" type="submit" :disabled="creating">{{ creating ? '正在创建…' : '创建画布' }}</button></div>
      </form>
    </dialog>
  </div>
</template>

<style scoped>
.home-page { height: 100%; overflow-y: auto; background: #111113; color: #f1f1ef; }
.home-header { height: 72px; padding: 0 clamp(24px, 3.6vw, 56px); display: flex; align-items: center; gap: 24px; border-bottom: 1px solid #303032; }
.home-header a { color: inherit; text-decoration: none; }
.home-header :deep(.account-control > button) { border-color: #454547; background: #252527; color: #e5e5e3; }
.home-header :deep(.account-control > button:hover:not(:disabled)) { background: #333335; }
.home-header :deep(.account-control .credit-pill) { border-color: #454547; background: #252527; color: #e5e5e3; }
.sign-in-button { margin-left: auto; padding: 9px 14px; border: 1px solid #4a4a4c; border-radius: 9px; background: transparent; color: #ededeb; font-size: 13px; cursor: pointer; }
.sign-in-button:hover { background: #252527; }
.home-main { max-width: 1520px; margin: 0 auto; padding: clamp(48px, 5vw, 76px) clamp(24px, 3.6vw, 56px) 80px; }
.library-heading { display: flex; align-items: center; justify-content: space-between; gap: 24px; margin-bottom: clamp(52px, 6vw, 84px); }
.library-heading h1 { margin: 0 0 14px; font-size: clamp(34px, 3.3vw, 50px); line-height: 1.16; font-weight: 600; letter-spacing: -.02em; }
.library-heading p, .create-dialog p { margin: 0; color: #a5a5a5; font-size: 14px; line-height: 1.7; }
.primary-button, .secondary-button { display: inline-flex; justify-content: center; align-items: center; gap: 8px; min-height: 42px; padding: 10px 18px; border-radius: 9px; font-size: 13px; cursor: pointer; white-space: nowrap; }
.primary-button { background: #f0efeb; border: 1px solid #f0efeb; color: #161617; font-weight: 600; }
.primary-button:hover:not(:disabled) { background: #d8d8d4; border-color: #d8d8d4; }
.primary-button > span { font-size: 20px; line-height: 16px; }
.secondary-button { background: #272729; border: 1px solid #48484a; color: #e8e8e6; }
.secondary-button:hover:not(:disabled) { background: #333335; }
.quiet-button { background: none; color: #aaa; border: 0; padding: 8px; cursor: pointer; font-size: 12px; }
.quiet-button:hover { color: #f1f1ef; }
.library-toolbar { display: flex; align-items: center; gap: 18px; margin-bottom: 28px; }
.library-toolbar h2 { flex: none; margin: 0; font-size: 20px; font-weight: 600; }
.library-rule { height: 1px; flex: 1; min-width: 16px; background: #303032; }
.library-toolbar > span { color: #a5a5a5; font-size: 12px; white-space: nowrap; }
.refresh-button { border: 1px solid #454547; border-radius: 8px; padding: 8px 12px; color: #d1d1cf; }
.refresh-button:hover:not(:disabled) { background: #252527; }
.canvas-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 28px; align-items: start; }
.canvas-card, .new-canvas-item { min-width: 0; }
.canvas-card { display: block; border-radius: 12px; color: inherit; text-decoration: none; }
.canvas-preview, .new-canvas-tile { width: 100%; aspect-ratio: 16 / 9; border-radius: 12px; }
.canvas-preview { position: relative; display: grid; place-items: center; overflow: hidden; background: #202022; transition: background 140ms; }
.canvas-card:hover .canvas-preview { background: #29292b; }
.canvas-card:focus-visible, .new-canvas-tile:focus-visible, .sign-in-button:focus-visible, a:focus-visible, input:focus-visible { outline: 2px solid #e7e7e3; outline-offset: 4px; }
.canvas-preview > svg:not(.empty-preview) { display: block; width: 100%; height: 100%; }
.canvas-preview rect { fill: #343436; stroke: #727274; stroke-width: .8; }
.canvas-preview g path { stroke: #727274; stroke-width: 1; }
.canvas-preview text { fill: #cececc; font-size: 9px; font-family: 'Microsoft YaHei', sans-serif; }
.preview-video rect { fill: #3b3b3d; }
.preview-audio rect { fill: #303032; }
.canvas-preview .empty-preview { width: 48px; height: 48px; color: #555557; }
.canvas-card-info { padding: 16px 5px 0; }
.canvas-card h3 { margin: 0 0 7px; overflow: hidden; color: #ececea; font-size: 15px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.canvas-card-meta { display: flex; justify-content: space-between; gap: 10px; color: #99999b; font-size: 12px; }
.new-canvas-tile { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 7px; border: 1px solid #626264; background: #242426; color: #f1f1ef; cursor: pointer; transition: background 140ms, border-color 140ms; }
.new-canvas-tile:hover:not(:disabled) { border-color: #929294; background: #2c2c2e; }
.new-frame { color: #929294; font-size: 34px; font-weight: 300; line-height: 1; }
.new-canvas-tile strong { font-size: 16px; font-weight: 600; }
.new-canvas-caption { display: block; padding: 16px 5px 0; color: #b5b5b5; font-size: 14px; }
.library-status, .empty-library, .session-notice { color: #a5a5a5; font-size: 13px; line-height: 1.8; margin: 0 0 24px; }
.library-notice { border: 1px solid #575044; background: #282620; padding: 22px; border-radius: 10px; margin-bottom: 24px; color: #e9d8b7; font-size: 13px; }
.library-notice h2 { font-size: 17px; font-weight: 500; }
.library-notice a { color: #dfdfdd; display: inline-block; margin-top: 10px; }
.temporary-hint { font-size: 12px; }
.more-canvases { display: flex; margin: 30px auto 0; }
.create-dialog { width: min(440px, calc(100vw - 32px)); padding: 28px; border: 1px solid #4d4d4f; border-radius: 14px; background: #242426; color: #f1f1ef; box-shadow: 0 24px 80px #0006; }
.create-dialog::backdrop { background: #080808c2; }
.dialog-heading { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.dialog-heading h2 { margin: 0; font-size: 20px; font-weight: 500; }
.dialog-heading button { font-size: 24px; padding: 0 5px; }
.create-dialog label { display: block; margin: 25px 0 10px; font-size: 13px; }
.create-dialog input { width: 100%; padding: 12px; background: #171719; border: 1px solid #555557; border-radius: 7px; color: #f1f1ef; font: inherit; font-size: 14px; }
.dialog-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 28px; }
.create-dialog .create-error { color: #efb6a5; margin-top: 14px; }
@media (max-width: 960px) { .canvas-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 600px) {
  .home-header { height: 68px; padding: 0 22px; }
  .home-main { padding: 42px 22px 54px; }
  .library-heading { align-items: flex-start; gap: 24px; margin-bottom: 48px; flex-direction: column; }
  .library-heading h1 { font-size: 34px; }
  .library-toolbar { flex-wrap: wrap; gap: 12px; }
  .library-toolbar h2 { font-size: 18px; }
  .library-toolbar > span { margin-left: 0; }
  .canvas-grid { grid-template-columns: minmax(0, 1fr); gap: 24px; }
}
@media (prefers-reduced-motion: reduce) { .canvas-preview, .new-canvas-tile { transition: none; } }
</style>
