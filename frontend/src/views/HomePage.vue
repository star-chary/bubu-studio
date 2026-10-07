<script setup lang="ts">
import AccountControl from '../components/AccountControl.vue'
import { nextTick, onMounted, ref } from 'vue'
import { persistenceRequest, writeJSON, type CanvasSummary, type SavedCanvas } from '../api/persistence'

const props = defineProps<{ autoOpenCreate?: boolean }>()
const emit = defineEmits<{ autoOpenHandled: [] }>()
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
onMounted(() => {
  void load()
  if (props.autoOpenCreate) void openCreate().finally(() => emit('autoOpenHandled'))
})
</script>

<template>
  <div class="home-page">
    <header class="home-header">
      <a class="brand" href="/" aria-label="帧间首页">
        <img class="brand-mark" src="/favicon.svg" alt="" width="34" height="34" />
        <span class="brand-name">帧间</span>
      </a>
      <AccountControl />
    </header>
    <main class="home-main">
      <div class="library-heading">
        <div><h1>我的画布</h1><p>从一个想法开始，或接着上次继续。</p></div>
        <button class="primary-button" type="button" :disabled="!enabled || loading" @click="openCreate">
          <span aria-hidden="true">＋</span> 新建画布
        </button>
      </div>
      <div class="library-toolbar">
        <h2>全部画布</h2><span>按最近修改排序</span>
        <button class="quiet-button" type="button" :disabled="loading" @click="load()">刷新列表</button>
      </div>
      <div v-if="error" class="library-notice" role="alert">
        <p>{{ error }}</p><button class="secondary-button" type="button" :disabled="loading" @click="load()">重新加载</button>
      </div>
      <p v-if="loading && !canvases.length" class="library-status" role="status">正在加载画布…</p>
      <div v-else-if="!enabled && !error" class="library-notice">
        <h2>画布保存服务未启用</h2>
        <p>启动数据库和后端后，便可新建画布并在这里继续编辑。</p>
        <a :href="temporaryURL">进入临时画布</a><p class="temporary-hint">临时内容在离开或刷新后不会保留。</p>
      </div>
      <template v-if="enabled">
        <p v-if="!loading && !error && !canvases.length" class="empty-library">还没有画布。新建一张，放入你的第一份素材。</p>
        <div class="canvas-grid">
          <button class="new-canvas-tile" type="button" :disabled="loading" @click="openCreate">
            <span class="new-frame" aria-hidden="true">＋</span>
            <strong>创建空白画布</strong><span>图片、视频与灵感，从这里展开</span>
          </button>
          <a v-for="canvas in canvases" :key="canvas.id" class="canvas-card" :href="`/?canvas=${canvas.id}`" :aria-label="`打开画布：${canvas.title}（${canvas.id.slice(0, 8)}）`">
            <div class="canvas-preview" aria-hidden="true">
              <svg viewBox="0 0 300 160" fill="none">
                <g v-for="(node, index) in preview(canvas)" :key="index" :class="`preview-${node.kind}`">
                  <rect :x="node.x" :y="node.y" :width="node.width" :height="node.height" rx="3" />
                  <path :d="`M${node.x + node.width * .2} ${node.y + node.height * .75}h${node.width * .6}`" />
                  <text v-if="node.width > 32" :x="node.x + node.width / 2" :y="node.y + node.height * .48" text-anchor="middle">{{ node.kind === 'image' ? '图片' : node.kind === 'video' ? '视频' : '音频' }}</text>
                </g>
                <g v-if="!canvas.nodeCount" class="empty-preview"><path d="M139 60h-13v13m35-13h13v13m-48 14v13h13m35-13v13h-13" /></g>
              </svg>
              <span class="card-open">继续编辑 <span aria-hidden="true">↗</span></span>
            </div>
            <div class="canvas-card-info">
              <h3 :title="canvas.title">{{ canvas.title }}</h3>
              <div class="canvas-card-meta"><span>{{ canvas.nodeCount ? `${canvas.nodeCount} 个节点` : '空白画布' }}</span><time :datetime="canvas.updatedAt">{{ formatDate(canvas.updatedAt) }}</time></div>
              <span class="canvas-id">{{ canvas.id.slice(0, 8) }}</span>
            </div>
          </a>
        </div>
        <button v-if="hasMore" class="secondary-button more-canvases" type="button" :disabled="loading" @click="load(true)">{{ loading ? '正在加载…' : '加载更多画布' }}</button>
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
.home-page { height: 100%; overflow-y: auto; background: var(--color-canvas); }
.home-header { height: 88px; padding: 0 clamp(24px, 5vw, 76px); display: flex; align-items: center; gap: 30px; border-bottom: 1px solid #292c32; }
.home-header a { color: inherit; text-decoration: none; }
.home-location { border-left: 1px solid var(--color-border); padding-left: 28px; color: var(--color-muted); font-size: 13px; }
.home-main { max-width: 1280px; margin: 0 auto; padding: 58px 40px 70px; }
.library-heading { display: flex; align-items: center; justify-content: space-between; gap: 24px; margin-bottom: 48px; }
.library-heading h1 { font-family: 'Microsoft YaHei', sans-serif; font-size: 30px; font-weight: 500; letter-spacing: 2px; margin: 0 0 13px; }
.library-heading p, .create-dialog p { color: var(--color-muted); font-size: 13px; line-height: 1.7; margin: 0; }
.primary-button, .secondary-button { display: inline-flex; justify-content: center; align-items: center; gap: 8px; min-height: 40px; padding: 10px 18px; border-radius: 8px; font-size: 13px; cursor: pointer; white-space: nowrap; }
.primary-button { background: #4f8eff; border: 1px solid #6c9fff; color: #081932; font-weight: 600; }
.primary-button:hover:not(:disabled) { background: #77a8ff; }
.primary-button > span { font-size: 20px; line-height: 16px; }
.secondary-button { background: var(--color-panel); border: 1px solid var(--color-border); color: var(--color-text); }
.quiet-button { background: none; color: var(--color-muted); border: 0; padding: 8px; cursor: pointer; font-size: 12px; }
.quiet-button:hover { color: var(--color-text); }
.library-toolbar { display: flex; align-items: center; gap: 20px; margin-bottom: 22px; }
.library-toolbar h2 { margin: 0 auto 0 0; font-size: 14px; font-weight: 500; }
.library-toolbar > span { color: var(--color-muted); font-size: 12px; }
.canvas-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 24px; }
.canvas-card, .new-canvas-tile { border-radius: 12px; overflow: hidden; min-width: 0; text-decoration: none; color: inherit; }
.canvas-card { border: 1px solid #30333b; background: #1b1d22; transition: border-color 140ms; }
.canvas-card:hover { border-color: #778598; }
.canvas-card:focus-visible, a:focus-visible, input:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 4px; }
.canvas-preview { position: relative; aspect-ratio: 300 / 160; background-color: #17191e; background-image: radial-gradient(#3b3e43 0.65px, transparent .65px); background-size: 14px 14px; }
.canvas-preview > svg { display: block; width: 100%; height: 100%; }
.canvas-preview rect { fill: #283647; stroke: #738faf; stroke-width: .8; }
.canvas-preview path { stroke: #738faf; stroke-width: 1; }
.canvas-preview text { fill: #c1d3e7; font-size: 9px; font-family: 'Microsoft YaHei', sans-serif; }
.preview-video rect { fill: #383348; stroke: #9385b1; }
.preview-audio rect { fill: #2b3d3b; stroke: #83a69f; }
.empty-preview path { stroke: #686e7b; }
.card-open { position: absolute; right: 12px; bottom: 12px; border: 1px solid #535d70; background: #202733; color: #d2e1ff; padding: 6px 9px; border-radius: 6px; font-size: 11px; opacity: 0; }
.canvas-card:hover .card-open, .canvas-card:focus-visible .card-open { opacity: 1; }
.canvas-card-info { position: relative; padding: 18px 20px 19px; }
.canvas-card h3 { margin: 0 0 11px; font-size: 15px; font-weight: 500; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; padding-right: 66px; }
.canvas-card-meta { display: flex; justify-content: space-between; gap: 10px; color: var(--color-muted); font-size: 11px; }
.canvas-id { position: absolute; right: 20px; top: 21px; color: #9299a5; font-family: Consolas, monospace; font-size: 10px; }
.new-canvas-tile { min-height: 270px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 13px; border: 1px dashed #444b58; background: #17191e; cursor: pointer; }
.new-canvas-tile:hover { border-color: #7da1d6; background: #1a1f29; }
.new-frame { display: grid; place-items: center; width: 50px; height: 50px; color: #91b6f1; border: 1px solid #485a74; border-radius: 9px; font-size: 25px; margin-bottom: 8px; }
.new-canvas-tile strong { font-size: 14px; font-weight: 500; }
.new-canvas-tile > span:last-child { font-size: 11px; color: var(--color-muted); }
.library-status, .empty-library { color: var(--color-muted); font-size: 13px; line-height: 1.8; margin: 28px 0; }
.library-notice { border: 1px solid #575044; background: #282620; padding: 22px; border-radius: 10px; margin-bottom: 24px; color: #e9d8b7; font-size: 13px; }
.library-notice h2 { font-size: 17px; font-weight: 500; }
.library-notice a { color: #b4cefb; display: inline-block; margin-top: 10px; }
.temporary-hint { font-size: 12px; }
.more-canvases { display: flex; margin: 30px auto 0; }
.create-dialog { width: min(440px, calc(100vw - 32px)); padding: 28px; border: 1px solid #454b57; border-radius: 14px; background: #22252b; color: var(--color-text); box-shadow: 0 24px 80px #0006; }
.create-dialog::backdrop { background: #080a0fc2; }
.dialog-heading { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.dialog-heading h2 { margin: 0; font-size: 20px; font-weight: 500; }
.dialog-heading button { font-size: 24px; padding: 0 5px; }
.create-dialog label { display: block; margin: 25px 0 10px; font-size: 13px; }
.create-dialog input { width: 100%; padding: 12px; background: #16191f; border: 1px solid #555d6b; border-radius: 7px; color: var(--color-text); font: inherit; font-size: 14px; }
.dialog-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 28px; }
.create-dialog .create-error { color: #efb6a5; margin-top: 14px; }
@media (max-width: 960px) { .canvas-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 600px) { .home-header { height: 72px; padding: 0 22px; } .home-main { padding: 32px 22px 44px; } .library-heading { align-items: flex-start; gap: 18px; margin-bottom: 30px; flex-direction: column; } .library-heading h1 { font-size: 26px; } .canvas-grid { grid-template-columns: minmax(0, 1fr); gap: 18px; } .new-canvas-tile { min-height: 205px; } .library-toolbar { gap: 10px; } }
@media (prefers-reduced-motion: reduce) { .canvas-card { transition: none; } }
</style>
