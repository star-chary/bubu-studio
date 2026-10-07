<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { MediaKind, MediaNodeData, MediaReference } from '../canvas/media'
import ImageModelSelect from './ImageModelSelect.vue'
import VideoGenerationControls from './VideoGenerationControls.vue'
import type { ImageModelId } from '../models/imageModels'
import { VIDEO_MODEL, normalizeVideoOptions, videoModelDefinition, type VideoMode, type VideoModelId, type VideoOptions } from '../models/videoModels'
import PromptEditor from './PromptEditor.vue'
import { generationStatusLabel } from '../canvas/generationStatus'
import { hasPromptReferences, type MentionCandidate, type PromptPart } from '../canvas/prompt'
import type { CreditQuote } from '../api/credits'

const props = defineProps<{
  nodeId: string
  nodeName: string
  kind: MediaKind
  modelValue: string
  promptParts?: PromptPart[]
  candidates: MentionCandidate[]
  addReference: (id: string) => string
  videoOptions?: VideoOptions
  videoModel?: VideoModelId
  videoMode?: VideoMode
  imageModel: ImageModelId
  references: MediaReference[]
  selectingReferences: boolean
  canGenerate: boolean
  generation?: MediaNodeData['generation']
  mediaLoading: boolean
  referenceError: string
  persistenceEnabled: boolean
  creditsAvailable: number | null
  creditsReserved: number | null
  creditsStatus: 'idle' | 'loading' | 'ready' | 'error'
  creditsError: string
  creditQuote: CreditQuote | null
  quoteStatus: 'idle' | 'loading' | 'ready' | 'error'
  quoteError: string
  anchor: { x: number; y: number; width: number; height: number }
}>()
const emit = defineEmits<{ 'update:promptParts': [value: PromptPart[]]; 'update:imageModel': [value: ImageModelId]; close: []; selectReferences: []; removeReference: [sourceId: string]; generate: []; retryStorage: []; retryQuote: []; 'update:videoModel': [value: VideoModelId]; 'update:videoMode': [value: VideoMode]; 'update:videoOptions': [value: VideoOptions] }>()
const panel = ref<HTMLElement | null>(null)
const input = ref<InstanceType<typeof PromptEditor> | null>(null)
const videoControls = ref<InstanceType<typeof VideoGenerationControls> | null>(null)
const expanded = ref(false)
const windowSize = ref({ width: window.innerWidth, height: window.innerHeight })
const title = computed(() => `${props.kind === 'image' ? '图片' : '视频'}提示词`)
const generating = computed(() => props.generation?.status === 'generating')
const promptLength = computed(() => [...props.modelValue.trim()].length)
const hasMentions = computed(() => hasPromptReferences(props.promptParts))
const mentionError = computed(() => props.kind === 'video' && props.videoMode === 'text' ? '' : props.promptParts?.some((part) => part.type === 'reference' && !props.candidates.some((c) => c.id === part.nodeId && c.selected && !c.error)) ? '提示词中的素材标签已失效，请重新选择参考素材' : '')
const selectedVideoModel = computed(() => props.videoModel ?? VIDEO_MODEL)
const selectedVideoMode = computed(() => props.videoMode ?? 'reference')
const videoDefinition = computed(() => videoModelDefinition(selectedVideoModel.value))
const video = computed(() => normalizeVideoOptions(selectedVideoModel.value, props.videoOptions))
const insufficientCredits = computed(() => !!props.creditQuote && props.creditsAvailable !== null && props.creditsAvailable < props.creditQuote.points)
const generateDisabled = computed(() => !!mentionError.value || generating.value || props.mediaLoading || props.selectingReferences || !!props.referenceError || ((props.kind === 'image' || (props.kind === 'video' && selectedVideoMode.value === 'text')) && !promptLength.value) || promptLength.value > 2000 || !props.persistenceEnabled || props.quoteStatus !== 'ready' || !props.creditQuote || props.creditsStatus !== 'ready' || insufficientCredits.value)
const creditHint = computed(() => {
  if (!props.persistenceEnabled) return '请先使用已保存画布，才能生成并使用测试积分。'
  if (props.quoteStatus === 'error') return `无法获取报价：${props.quoteError}`
  if (props.quoteStatus === 'loading') return '正在计算本次积分…'
  if (!props.creditQuote) return '填写有效内容后显示本次积分。'
  if (props.creditsStatus === 'error') return `无法读取余额：${props.creditsError}`
  if (props.creditsStatus !== 'ready') return `本次需要 ${props.creditQuote.points} 积分 · 正在读取余额…`
  return `本次需要 ${props.creditQuote.points} 积分 · 可用 ${props.creditsAvailable} · 冻结 ${props.creditsReserved}${insufficientCredits.value ? ' · 积分不足，无法生成' : ''}`
})
const creditAmount = computed(() => props.quoteStatus === 'ready' && props.creditQuote ? String(props.creditQuote.points) : props.quoteStatus === 'loading' ? '…' : '—')
const creditNotice = computed(() => {
  if (!props.persistenceEnabled) return '请先使用已保存画布'
  if (props.quoteStatus === 'error') return '报价获取失败'
  if (props.creditsStatus === 'error') return '余额读取失败'
  if (insufficientCredits.value) return '积分不足'
  return ''
})
const generateLabel = computed(() => generating.value ? generationStatusLabel(props.generation) : props.mediaLoading ? '加载中…' : props.kind === 'image' ? '生成图片' : '生成视频')
const referenceSummary = computed(() => {
  const max = videoDefinition.value.maxReferences
  return `图片 ${props.references.filter((r) => r.kind === 'image').length}/${max.image} · 视频 ${props.references.filter((r) => r.kind === 'video').length}/${max.video} · 音频 ${props.references.filter((r) => r.kind === 'audio').length}/${max.audio}`
})

const layout = computed(() => {
  const margin = 16
  const width = Math.min(expanded.value ? 840 : 660, windowSize.value.width - margin * 2)
  const videoHeight = (windowSize.value.width <= 600 ? 306 : 242) + (props.references.length ? 70 : 0)
  const height = Math.min(expanded.value ? 468 : props.kind === 'video' ? videoHeight + 28 : props.references.length ? 282 : 222, windowSize.value.height - margin * 2)
  const maxX = windowSize.value.width - width - margin
  const maxY = windowSize.value.height - height - margin
  const below = props.anchor.y + props.anchor.height + 12
  const above = props.anchor.y - height - 12
  const x = expanded.value ? (windowSize.value.width - width) / 2 : props.anchor.x + (props.anchor.width - width) / 2
  const y = expanded.value ? (windowSize.value.height - height) / 2 : below <= maxY ? below : above
  return {
    left: `${Math.max(margin, Math.min(x, maxX))}px`,
    top: `${Math.max(margin, Math.min(y, maxY))}px`,
    width: `${width}px`,
    height: `${height}px`,
  }
})

watch(() => props.nodeId, async () => {
  expanded.value = false
  await nextTick()
  input.value?.focus()
}, { immediate: true })

watch(() => props.selectingReferences, async (selecting) => {
  // 进入选择时收起放大的编辑器，让画布素材可见。
  if (selecting) {
    input.value?.closeMenu()
    videoControls.value?.close()
    expanded.value = false
    return
  }
  // 来源节点和移除按钮都可能夺走焦点，返回编辑时恢复键盘操作。
  await nextTick()
  input.value?.focus()
})

async function toggleExpanded() {
  const selection = input.value?.capture()
  input.value?.closeMenu()
  videoControls.value?.close()
  expanded.value = !expanded.value
  await nextTick()
  input.value?.focus()
  if (selection) input.value?.restore(selection)
}

function closeOnOutside(event: PointerEvent) {
  if (props.selectingReferences) return
  if (event.target instanceof Node && !panel.value?.contains(event.target)) emit('close')
}

function handleKeydown(event: KeyboardEvent) {
  // Esc 先交给中文输入法处理正在组词的内容。
  if (event.key === 'Escape' && !event.isComposing) {
    event.preventDefault()
    emit('close')
  }
}

function resize() {
  windowSize.value = { width: window.innerWidth, height: window.innerHeight }
}

onMounted(() => {
  document.addEventListener('pointerdown', closeOnOutside, true)
  window.addEventListener('resize', resize)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', closeOnOutside, true)
  window.removeEventListener('resize', resize)
})
</script>

<template>
  <section
    ref="panel"
    class="node-prompt-panel"
    :class="{ 'is-expanded': expanded, 'is-video': kind === 'video' }"
    :style="layout"
    role="dialog"
    aria-labelledby="node-prompt-title"
    @pointerdown.stop
    @mousedown.stop
    @wheel.stop
    @keydown.stop="handleKeydown"
  >
    <header class="prompt-panel-header">
      <h2 id="node-prompt-title" class="prompt-accessible-title">{{ title }}</h2>
      <button class="prompt-reference-button" :class="{ 'is-active': selectingReferences }" type="button" :aria-pressed="selectingReferences" :disabled="kind === 'video' && selectedVideoMode === 'text'" :title="kind === 'video' && selectedVideoMode === 'text' ? '切换到全能参考后添加素材' : '添加参考素材'" @click="emit('selectReferences')">
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 3v10M3 8h10" /></svg>
        参考
      </button>
      <span class="prompt-node-label" :title="nodeName">
        <svg v-if="kind === 'image'" viewBox="0 0 20 20" aria-hidden="true">
          <rect x="3" y="3" width="14" height="14" rx="2" />
          <path d="m3 13 4-5 6 6 2-2 2 2" />
          <circle cx="13" cy="7" r="1" />
        </svg>
        <svg v-else viewBox="0 0 20 20" aria-hidden="true"><path d="m6 4 9 6-9 6z" /></svg>
        <span>{{ nodeName }}</span>
      </span>
      <button
        type="button"
        class="prompt-expand-button"
        :aria-label="expanded ? '恢复输入框大小' : '放大输入框'"
        :title="expanded ? '恢复大小' : '放大输入框'"
        :aria-expanded="expanded"
        @click="toggleExpanded"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path v-if="expanded" d="M4 10h6V4m0 6L3 3m17 11h-6v6m0-6 7 7" />
          <path v-else d="M15 3h6v6m0-6-7 7M9 21H3v-6m0 6 7-7" />
        </svg>
      </button>
    </header>
    <ol v-if="references.length" class="prompt-references" :class="{ 'is-inactive': kind === 'video' && selectedVideoMode === 'text' }" :aria-label="kind === 'video' && selectedVideoMode === 'text' ? '已连接但本次不使用的参考素材' : '已选参考'">
      <li v-for="reference in references" :key="reference.id" class="prompt-reference" :class="{ 'has-error': reference.error }" :title="`${reference.label} · ${reference.name}${reference.error ? `：${reference.error}` : ''}`">
        <img v-if="reference.kind === 'image' && reference.url" :src="reference.url" :alt="reference.name" :draggable="false" />
        <template v-else-if="reference.kind === 'video' && reference.url">
          <video :src="reference.url" :aria-label="reference.name" muted playsinline preload="metadata" tabindex="-1"></video>
          <svg class="reference-video-mark" viewBox="0 0 16 16" aria-hidden="true"><path d="m5 3 8 5-8 5Z" /></svg>
        </template>
        <span v-else class="reference-audio-mark" :aria-label="reference.name">{{ reference.kind === 'audio' ? '♫' : '◇' }}</span>
        <span class="reference-index">{{ reference.label }}</span>
        <span v-if="reference.error" class="reference-warning" :aria-label="reference.error">!</span>
        <button class="reference-remove" type="button" :aria-label="`移除参考 ${reference.name}`" @click="emit('removeReference', reference.id)">×</button>
      </li>
    </ol>
    <PromptEditor
      :key="nodeId"
      ref="input"
      :node-id="nodeId" :label="title"
      :placeholder="kind === 'image' ? '描述画面内容、风格和细节，输入 @ 引用素材' : selectedVideoMode === 'text' ? '描述视频画面、运动和镜头，不使用参考素材' : '描述视频画面与运动，输入 @ 引用素材'"
      :model-value="modelValue" :parts="promptParts" :candidates="kind === 'video' && selectedVideoMode === 'text' ? [] : candidates" :add-reference="addReference"
      @change="emit('update:promptParts', $event)"
    />
    <p v-if="mentionError" class="generation-hint mention-generation-status" role="status">{{ mentionError }}</p>
    <p v-if="generation?.pollingError" class="generation-hint" role="status">{{ generation.pollingError }}</p>
    <button v-if="generation?.taskStatus === 'storage_failed'" class="retry-video-storage" type="button" @click="emit('retryStorage')">重试保存视频</button>
    <p v-if="generation?.error" class="generation-error" role="alert">{{ generation.error }}</p>
    <p v-if="canGenerate && ((kind === 'image' && referenceError) || promptLength > 2000)" class="generation-hint">{{ promptLength > 2000 ? '提示词最多 2000 字' : referenceError }}</p>
    <p v-else-if="canGenerate && kind === 'image' && references.length && !hasMentions" class="generation-hint">将使用以上 {{ references.length }} 张参考图片，生成单张图片</p>
    <template v-if="kind === 'video'">
      <template v-if="selectedVideoMode === 'reference'">
        <p class="generation-hint">{{ referenceSummary }} · 视频、音频各 ≤{{ videoDefinition.maxReferenceSeconds }} 秒</p>
        <p class="generation-hint video-reference-status" :class="{ 'has-error': referenceError }" role="status">{{ referenceError || '参考素材检查通过' }}</p>
      </template>
      <p v-else class="generation-hint video-reference-status" :class="{ 'has-error': referenceError }" role="status">{{ referenceError || (references.length ? '文生视频不会使用已连接参考素材；切回全能参考可继续使用' : '文生视频仅使用提示词，不读取参考素材') }}</p>
    </template>
    <p v-if="canGenerate && creditNotice" class="generation-hint credit-notice" role="status">{{ creditNotice }}</p>
    <button v-if="canGenerate && quoteStatus === 'error' && persistenceEnabled" class="retry-quote" type="button" @click="emit('retryQuote')">重新获取报价</button>
    <footer v-if="canGenerate" class="prompt-generation-footer">
      <div class="generation-options">
        <ImageModelSelect v-if="kind === 'image'" :key="nodeId" :model-value="imageModel" :panel-position="layout" @update:model-value="emit('update:imageModel', $event)" />
        <span v-if="kind === 'image'" class="generation-description">2K · 单张</span>
        <VideoGenerationControls v-else :key="nodeId" ref="videoControls" :model-value="video" :video-model="selectedVideoModel" :video-mode="selectedVideoMode" :reference-count="references.length" :reference-error="referenceError" :panel-position="layout" @update:model-value="emit('update:videoOptions', $event)" @update:video-model="emit('update:videoModel', $event)" @update:video-mode="emit('update:videoMode', $event)" />
      </div>
      <div class="generation-actions">
        <details :key="nodeId" class="credit-cost" :class="{ 'has-error': !!creditNotice }">
          <summary class="credit-cost-trigger" :aria-label="`查看本次积分明细：${creditHint}`" title="查看本次积分明细">
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8.7 1.5 3.8 8h3.5l-.6 6.5 5.5-7H8.6z" /></svg>
            <span aria-live="polite">{{ creditAmount }}</span>
          </summary>
          <div class="credit-cost-popover">{{ creditHint }}</div>
        </details>
        <button type="button" class="generate-button" :disabled="generateDisabled" :aria-label="generateLabel" :title="generateLabel" @click="emit('generate')">
          <span v-if="generating" class="generation-spinner" aria-hidden="true"></span>
          <svg v-else viewBox="0 0 20 20" aria-hidden="true"><path d="M10 15V5m-4 4 4-4 4 4" /></svg>
        </button>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.retry-video-storage { align-self: flex-start; margin-top: 6px; border: 1px solid #888; border-radius: 6px; padding: 5px 10px; background: #353535; color: #fff; cursor: pointer; }

.node-prompt-panel { position: fixed; z-index: 15; display: flex; flex-direction: column; padding: 12px 16px 18px; border: 1px solid #383838; border-radius: 18px; background: #262626; box-shadow: 0 8px 24px #0003; }
.node-prompt-panel:focus-within { border-color: #505050; }
.prompt-panel-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex: none; }
.prompt-reference-button { display: inline-flex; flex: none; align-items: center; gap: 3px; height: 28px; padding: 0 9px 0 6px; border: 0; border-radius: 14px; background: #353535; color: #d6d6d6; cursor: pointer; font-size: 12px; }
.prompt-reference-button:hover { background: #454545; color: #fff; }
.prompt-reference-button.is-active { background: #29436b; color: #bdd4ff; }
.prompt-reference-button:disabled { opacity: .5; cursor: not-allowed; }
.prompt-reference-button svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.2; stroke-linecap: round; }
.prompt-references { display: flex; flex: none; gap: 10px; overflow-x: auto; max-width: 100%; margin: 8px 0 0; padding: 4px 2px; list-style: none; scrollbar-width: thin; scrollbar-color: #555 transparent; }
.prompt-references.is-inactive { opacity: .52; }
.prompt-reference { position: relative; flex: none; width: 48px; height: 48px; border: 1px solid #525252; border-radius: 8px; background: #1a1a1a; }
.prompt-reference.has-error { border-color: #ad875a; }
.reference-audio-mark { display: grid; place-items: center; height: 100%; color: #bed0e8; font-size: 26px; }
.reference-warning { position: absolute; right: 3px; bottom: 2px; color: #efd0a7; font-size: 13px; font-weight: 700; }
.video-reference-status { max-height: 42px; overflow-y: auto; line-height: 1.6; }
.video-reference-status.has-error { color: #efd0a7; }
.prompt-reference img, .prompt-reference video { display: block; width: 100%; height: 100%; object-fit: cover; border-radius: 7px; pointer-events: none; }
.reference-index { position: absolute; top: 2px; left: 2px; min-width: 14px; padding: 0 3px; border-radius: 4px; background: #101010bd; color: #eee; font-size: 10px; line-height: 14px; text-align: center; }
.reference-remove { position: absolute; top: -4px; right: -4px; display: grid; place-items: center; width: 18px; height: 18px; padding: 0; border: 1px solid #666; border-radius: 50%; background: #333; color: #fff; cursor: pointer; font-size: 14px; line-height: 1; }
.reference-remove:hover { background: #5a3333; }
.reference-video-mark { position: absolute; right: 4px; bottom: 4px; width: 14px; height: 14px; fill: #fff; filter: drop-shadow(0 1px 2px #000); pointer-events: none; }
.prompt-accessible-title { position: absolute; width: 1px; height: 1px; overflow: hidden; margin: -1px; padding: 0; clip-path: inset(50%); white-space: nowrap; }
.prompt-node-label { display: inline-flex; align-items: center; gap: 6px; min-width: 0; margin-right: auto; height: 28px; padding: 0 9px; border-radius: 14px; background: #333; color: #b2b2b2; font-size: 12px; line-height: 1; }
.prompt-node-label > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.prompt-node-label svg { flex: none; width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 1.3; stroke-linecap: round; stroke-linejoin: round; }
.prompt-expand-button { display: flex; align-items: center; justify-content: center; flex: none; width: 28px; height: 28px; margin-right: -4px; border: 0; border-radius: 6px; background: transparent; color: #999; cursor: pointer; }
.prompt-expand-button:hover { background: #373737; color: #eee; }
.prompt-expand-button svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }
.mention-generation-status { color: #cbb69c; }
.prompt-generation-footer { display: flex; flex: none; align-items: center; justify-content: space-between; gap: 12px; margin-top: 10px; }
.generation-options { display: flex; align-items: center; gap: 12px; min-width: 0; }
.generation-description { flex: none; padding-left: 12px; border-left: 1px solid #494949; color: #aaa; font-size: 12px; }
.generation-hint { flex: none; margin: 6px 0 0; color: #bbb; font-size: 12px; }
.credit-notice { color: #f0b8a9; }
.retry-quote { align-self: flex-start; margin-top: 5px; border: 0; padding: 0; background: none; color: #bdd4ff; font-size: 12px; cursor: pointer; }
@media (max-width: 600px) { .generation-description { display: none; } }
.generation-actions { display: inline-flex; flex: none; align-items: center; gap: 7px; margin-left: auto; }
.credit-cost { position: relative; flex: none; color: #a6a6a6; }
.credit-cost.has-error { color: #e2b296; }
.credit-cost-trigger { display: inline-flex; align-items: center; gap: 3px; min-height: 32px; padding: 0 2px; border-radius: 6px; font-size: 12px; line-height: 1; cursor: pointer; list-style: none; white-space: nowrap; font-variant-numeric: tabular-nums; }
.credit-cost-trigger::-webkit-details-marker { display: none; }
.credit-cost-trigger:hover, .credit-cost[open] .credit-cost-trigger { color: #f1f1f1; }
.credit-cost-trigger:focus-visible, .generate-button:focus-visible { outline: 2px solid #bdd4ff; outline-offset: 2px; }
.credit-cost-trigger svg { width: 13px; height: 13px; fill: currentColor; }
.credit-cost-popover { position: absolute; right: 0; bottom: calc(100% + 9px); z-index: 2; width: min(250px, calc(100vw - 48px)); padding: 11px 12px; border: 1px solid #505050; border-radius: 10px; background: #303030; box-shadow: 0 8px 22px #0006; color: #e5e5e5; font-size: 12px; line-height: 1.6; }
.generate-button { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 34px; height: 34px; padding: 0; border: 0; border-radius: 10px; background: #e4e4e4; color: #202020; cursor: pointer; }
.generate-button:hover:not(:disabled) { background: #fff; }
.generate-button:disabled { background: #858585; color: #242424; cursor: not-allowed; }
.generate-button svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
.generation-error { flex: none; max-height: 48px; overflow-y: auto; margin: 8px 0 0; color: #edb6af; font-size: 12px; line-height: 1.6; }
.generation-spinner { width: 13px; height: 13px; border: 1.5px solid #666; border-right-color: transparent; border-radius: 50%; animation: generation-spin 1s linear infinite; }
@keyframes generation-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .generation-spinner { animation: none; } }
</style>
