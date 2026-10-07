<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Background } from '@vue-flow/background'
import { useVueFlow, VueFlow, type NodeMouseEvent, type XYPosition } from '@vue-flow/core'
import CanvasContextMenu from '../components/CanvasContextMenu.vue'
import { auth } from '../auth/session'
import AccountControl from '../components/AccountControl.vue'
import CanvasToolbar from '../components/CanvasToolbar.vue'
import MediaNode from '../components/MediaNode.vue'
import NodePromptPanel from '../components/NodePromptPanel.vue'
import ReferenceSelectionBar from '../components/ReferenceSelectionBar.vue'
import TaskHistory from '../components/TaskHistory.vue'
import { useMediaNodes } from '../canvas/useMediaNodes'
import { useNodeReferences } from '../canvas/useNodeReferences'
import type { MediaKind } from '../canvas/media'
import { DEFAULT_IMAGE_MODEL } from '../models/imageModels'
import { INITIAL_VIEWPORT, MAX_ZOOM, MIN_ZOOM } from '../canvas/viewport'
import { credits, quoteGeneration, refreshCredits, type CreditQuote } from '../api/credits'

// 画布和工具栏共享这个实例，位置与缩放只在 Vue Flow 中维护。
const props = defineProps<{ canvasId: string; temporary: boolean }>()
const flowId = `canvas-${props.canvasId}`
const { viewport, zoomIn, zoomOut, setViewport, setCenter, screenToFlowCoordinate, flowToScreenCoordinate } = useVueFlow(flowId)
const { nodes, session, importNotice, createEmptyNode, importFiles, mediaReady, failMedia, updatePrompt, updateImageModel, updateVideoModel, updateVideoMode, updateVideoOptions, generationInputFor, generateNode, retryVideoStorage, saveUploadedNode, referenceErrorFor, hasLocalMedia, isSubmitting } = useMediaNodes(flowId, props.canvasId, props.temporary)
const { title, ready, enabled: persistenceEnabled, loadError, saveError, trackingError, saveLabel, saving, conflict, historyOpen, history, hasMore, historyLoading } = session
const { targetNode: referenceTarget, isSelecting, feedback, referenceEdges, referencesFor, beginSelection, finishSelection, selectReference, removeReference, candidateState, mentionCandidatesFor, addMentionReference } = useNodeReferences(flowId, session.canvasId)
const leaving = ref(false)
const leaveError = ref('')
const leaveDialog = ref<HTMLDialogElement | null>(null)
let navigationApproved = false
function protectLocalMedia(event: BeforeUnloadEvent) {
  if (!navigationApproved && (hasLocalMedia() || (props.temporary && nodes.value.length) || isSubmitting())) {
    event.preventDefault()
    event.returnValue = ''
  }
}
onMounted(() => window.addEventListener('beforeunload', protectLocalMedia))
onBeforeUnmount(() => window.removeEventListener('beforeunload', protectLocalMedia))

async function returnHome(discardLocal = false) {
  if (leaving.value) return
  leaveError.value = ''
  if (ready.value && isSubmitting()) { leaveError.value = '正在提交生成任务，请稍后返回首页。'; return }
  if (!discardLocal && (hasLocalMedia() || (props.temporary && nodes.value.length))) {
    leaveDialog.value?.showModal()
    return
  }
  leaving.value = true
  leaveDialog.value?.close()
  try {
    if (ready.value) await session.flush()
    navigationApproved = true
    location.assign('/')
  } catch { leaveError.value = '画布尚未保存，已留在当前页面。请恢复连接并保存后再返回首页。'; leaving.value = false }
}
async function prepareLogout() {
  if (isSubmitting()) throw new Error('正在提交生成任务，请稍后退出。')
  if (hasLocalMedia() && !window.confirm('有尚未上传的本地素材，退出后无法恢复。仍要退出吗？')) return false
  if (ready.value) await session.flush()
  navigationApproved = true
  return true
}
const activePromptId = ref<string | null>(null)
watch(() => auth.status, (status) => { if (status !== 'authenticated') { activePromptId.value = null; leaveDialog.value?.close() } })
const activePromptNode = computed(() => nodes.value.find((node) => node.id === activePromptId.value))
const activeReferences = computed(() => activePromptId.value ? referencesFor(activePromptId.value) : [])
const quoteRequest = computed(() => {
  const node = activePromptNode.value
  if (auth.status !== 'authenticated' || !node || node.data.kind === 'audio') return null
  const input = generationInputFor(node.id)
  if (!input) return null
  const kind = node.data.kind
  return { kind, input, key: JSON.stringify({ kind, ...input }) }
})
const creditQuote = ref<CreditQuote | null>(null)
const quoteStatus = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
const quoteError = ref('')
let quotedKey = ''
let quoteSequence = 0
let quoteTimer: ReturnType<typeof setTimeout> | undefined
let quoteController: AbortController | undefined
function refreshQuote() {
  const request = quoteRequest.value
  quoteSequence++
  if (quoteTimer) clearTimeout(quoteTimer)
  quoteController?.abort()
  creditQuote.value = null
  quoteError.value = ''
  quotedKey = ''
  if (!request) { quoteStatus.value = 'idle'; return }
  quoteStatus.value = 'loading'
  const sequence = quoteSequence
  quoteTimer = setTimeout(async () => {
    const controller = new AbortController()
    quoteController = controller
    try {
      // 后端按已保存的节点和参考素材校验报价；新建节点必须先落库。
      await session.flush()
      if (sequence !== quoteSequence || controller.signal.aborted) return
      const result = await quoteGeneration(request.kind, request.input, controller.signal)
      if (sequence !== quoteSequence || controller.signal.aborted) return
      creditQuote.value = result
      quotedKey = request.key
      quoteStatus.value = 'ready'
    } catch (cause) {
      if (sequence !== quoteSequence || controller.signal.aborted) return
      quoteStatus.value = 'error'
      quoteError.value = cause instanceof Error ? cause.message : '暂时无法获取积分报价。'
    }
  }, 250)
}
watch(() => quoteRequest.value?.key, refreshQuote, { immediate: true })
watch(saveError, (error, previous) => { if (previous && !error && quoteStatus.value === 'error') refreshQuote() })
onBeforeUnmount(() => { quoteSequence++; if (quoteTimer) clearTimeout(quoteTimer); quoteController?.abort() })
async function generateActiveNode() {
  const node = activePromptNode.value
  const request = quoteRequest.value
  const quote = creditQuote.value
  if (!node || !request || !quote || quoteStatus.value !== 'ready' || quotedKey !== request.key || credits.status !== 'ready' || credits.available === null || credits.available < quote.points) return
  await generateNode(node.id, { input: request.input, quote })
  await refreshCredits()
  if (quoteRequest.value?.key === request.key) refreshQuote()
}
const referenceSources = computed(() => new Set(referenceEdges.value.map((edge) => edge.source)))
const promptAnchor = computed(() => {
  const node = activePromptNode.value
  if (!node) return null
  return {
    ...flowToScreenCoordinate(node.computedPosition),
    width: node.dimensions.width * viewport.value.zoom,
    height: node.dimensions.height * viewport.value.zoom,
  }
})
const contextMenuPosition = ref<{ x: number; y: number } | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
let pendingUploadPosition: XYPosition | null = null

function openPrompt({ event, node }: NodeMouseEvent) {
  // 播放和进度控件有自己的操作，不把这些点击当成编辑节点。
  if (event.target instanceof Element && event.target.closest('button, input, textarea, a')) return
  if (node.type !== 'media') return
  contextMenuPosition.value = null
  if (isSelecting.value) {
    selectReference(node.id)
    return
  }
  if (node.data.kind === 'audio') { activePromptId.value = null; return }
  activePromptId.value = node.id
}

function closePrompt() {
  if (!isSelecting.value) activePromptId.value = null
}

function selectReferences() {
  if (isSelecting.value) {
    finishSelection()
    return
  }
  if (activePromptId.value) beginSelection(activePromptId.value)
}

function returnToNode() {
  const node = referenceTarget.value
  finishSelection()
  if (!node) return
  activePromptId.value = node.id
  void setCenter(node.computedPosition.x + node.dimensions.width / 2, node.computedPosition.y + node.dimensions.height / 2, { zoom: viewport.value.zoom })
}

function endSelectionOnEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.isComposing || !isSelecting.value) return
  event.preventDefault()
  event.stopPropagation()
  finishSelection()
}

onMounted(() => document.addEventListener('keydown', endSelectionOnEscape, true))
onBeforeUnmount(() => document.removeEventListener('keydown', endSelectionOnEscape, true))

function addNode(kind: Exclude<MediaKind, 'audio'>) {
  if (!contextMenuPosition.value) return
  createEmptyNode(kind, screenToFlowCoordinate(contextMenuPosition.value))
  contextMenuPosition.value = null
}

function chooseFiles() {
  if (!contextMenuPosition.value || !fileInput.value) return
  // 打开文件选择器前保存画布坐标，异步选完文件仍落在原来的创建位置。
  pendingUploadPosition = screenToFlowCoordinate(contextMenuPosition.value)
  contextMenuPosition.value = null
  fileInput.value.value = ''
  fileInput.value.click()
}

function filesSelected(event: Event) {
  const input = event.currentTarget as HTMLInputElement
  if (pendingUploadPosition && input.files?.length) {
    importFiles(Array.from(input.files), pendingUploadPosition)
  }
  pendingUploadPosition = null
  input.value = ''
}

function openContextMenu(event: MouseEvent) {
  event.preventDefault()
  if (isSelecting.value || !ready.value) return
  activePromptId.value = null
  contextMenuPosition.value = { x: event.clientX, y: event.clientY }
}

function resetViewport() {
  void setViewport({ ...INITIAL_VIEWPORT })
}
</script>

<template>
  <main class="canvas-page" aria-label="帧间画布" aria-describedby="canvas-instructions">
    <VueFlow
      :id="flowId"
      :edges="referenceEdges"
      :nodes-connectable="false"
      :nodes-draggable="!isSelecting"
      :node-drag-threshold="0"
      :default-viewport="INITIAL_VIEWPORT"
      :min-zoom="MIN_ZOOM"
      :max-zoom="MAX_ZOOM"
      :pan-on-drag="[0, 1]"
      :pan-on-scroll="false"
      :zoom-on-scroll="true"
      :zoom-on-pinch="true"
      :zoom-on-double-click="false"
      :selection-key-code="null"
      :multi-selection-key-code="null"
      :delete-key-code="null"
      :prevent-scrolling="true"
      :disable-keyboard-a11y="true"
      @pane-context-menu="openContextMenu"
      @node-click="openPrompt"
      @node-drag-start="closePrompt"
      @pane-click="closePrompt"
    >
      <Background :gap="24" :size="1" pattern-color="#393c43" />
      <template #node-media="{ id, data, selected }">
        <MediaNode :data="data" :selected="activePromptId ? activePromptId === id : selected" :reference-state="candidateState(id)" :has-references="!!data.referenceIds?.length" :is-reference-source="referenceSources.has(id)" @loaded="mediaReady(id, $event)" @failed="failMedia(id)" @retry-save="saveUploadedNode(id)" />
      </template>
    </VueFlow>

    <!-- 固定界面与可变换画布分层，工具栏不会跟随视口缩放或触发拖动。 -->
    <header class="canvas-header">
      <button class="home-button" type="button" aria-label="返回首页" :disabled="leaving" @click="returnHome()">
        <span aria-hidden="true">‹</span>
        <img class="brand-mark" src="/favicon.svg" alt="" width="38" height="38" />
        <span>首页</span>
      </button>
      <span class="header-divider" aria-hidden="true"></span>
      <h1 :title="title">{{ title }}</h1>
      <AccountControl :before-logout="prepareLogout" />
    </header>

    <div class="save-controls" @pointerdown.stop>
      <span role="status" aria-label="画布保存状态">{{ saveLabel }}</span>
      <button v-if="persistenceEnabled" type="button" :disabled="!ready || saving || conflict" @click="session.flush().catch(() => {})">保存</button>
      <button v-if="persistenceEnabled" type="button" :aria-expanded="historyOpen" @click="historyOpen = !historyOpen">任务记录</button>
    </div>
    <div v-if="!ready" class="restore-overlay" role="status">
      <p>{{ loadError || '正在恢复画布…' }}</p>
      <button v-if="loadError" type="button" @click="session.initialize()">重新连接</button>
      <button v-if="loadError" type="button" @click="returnHome()">返回首页</button>
    </div>
    <p v-if="leaveError || saveError || trackingError" class="persistence-notice" role="alert">{{ leaveError || saveError || trackingError }}</p>
    <dialog ref="leaveDialog" class="leave-dialog" aria-labelledby="leave-title">
      <h2 id="leave-title">还有内容未保存</h2>
      <p>{{ temporary ? '临时画布的内容在离开后不会保留。' : '部分本地素材尚未上传成功。返回首页会停止上传，下次打开需要重新上传这些素材。' }}</p>
      <div><button type="button" @click="leaveDialog?.close()">继续编辑</button><button type="button" @click="returnHome(true)">仍然返回首页</button></div>
    </dialog>
    <TaskHistory v-if="historyOpen && auth.status === 'authenticated'" :tasks="history" :has-more="hasMore" :loading="historyLoading" :error="trackingError" @close="historyOpen = false" @retry-storage="retryVideoStorage($event)" @refresh="session.loadHistory()" @more="session.loadHistory(true)" />

    <div v-if="!nodes.length" class="empty-guide" aria-hidden="true">
      <svg class="frame-mark" viewBox="0 0 64 64" fill="none">
        <path d="M22 10H10v12m32-12h12v12M10 42v12h12m32-12v12H42" />
        <path class="frame-cross" d="M26 32h12m-6-6v12" />
      </svg>
      <p class="empty-title">拖动画布，换个视角</p>
      <p class="empty-description">从这里，自由探索你的创作空间</p>
    </div>

    <p class="gesture-hint" id="canvas-instructions">
      <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <rect x="5" y="2" width="10" height="16" rx="5" />
        <path d="M10 5v4" />
      </svg>
      <span>按住空白处拖动<span class="hint-separator">·</span>滚轮缩放</span>
    </p>

    <CanvasToolbar
      :zoom="viewport.zoom"
      @zoom-in="zoomIn()"
      @zoom-out="zoomOut()"
      @reset="resetViewport"
    />
    <ReferenceSelectionBar
      v-if="referenceTarget"
      :kind="referenceTarget.data.kind"
      :count="activeReferences.length"
      :feedback="feedback"
      @return-to-node="returnToNode"
      @close="finishSelection"
    />
    <NodePromptPanel
      v-if="activePromptNode && promptAnchor && auth.status === 'authenticated'"
      :node-id="activePromptNode.id"
      :node-name="activePromptNode.data.name"
      :kind="activePromptNode.data.kind"
      :anchor="promptAnchor"
      :model-value="activePromptNode.data.prompt ?? ''"
      :prompt-parts="activePromptNode.data.promptParts"
      :candidates="mentionCandidatesFor(activePromptNode.id)"
      :add-reference="(sourceId) => addMentionReference(activePromptNode!.id, sourceId)"
      :video-options="activePromptNode.data.videoOptions"
      :video-model="activePromptNode.data.videoModel"
      :video-mode="activePromptNode.data.videoMode"
      :image-model="activePromptNode.data.imageModel ?? DEFAULT_IMAGE_MODEL"
      :references="activeReferences"
      :selecting-references="isSelecting"
      :can-generate="activePromptNode.data.origin !== 'upload'"
      :generation="activePromptNode.data.generation"
      :media-loading="activePromptNode.data.status === 'loading'"
      :reference-error="referenceErrorFor(activePromptNode.id)"
      :persistence-enabled="persistenceEnabled"
      :credits-available="credits.available"
      :credits-reserved="credits.reserved"
      :credits-status="credits.status"
      :credits-error="credits.error"
      :credit-quote="creditQuote"
      :quote-status="quoteStatus"
      :quote-error="quoteError"
      @update:prompt-parts="updatePrompt(activePromptNode.id, $event)"
      @update:video-options="updateVideoOptions(activePromptNode.id, $event)"
      @update:video-model="updateVideoModel(activePromptNode.id, $event)"
      @update:video-mode="updateVideoMode(activePromptNode.id, $event)"
      @update:image-model="updateImageModel(activePromptNode.id, $event)"
      @select-references="selectReferences"
      @remove-reference="removeReference(activePromptNode.id, $event)"
      @generate="generateActiveNode"
      @retry-quote="refreshQuote"
      @retry-storage="activePromptNode.data.generation?.taskId && retryVideoStorage(activePromptNode.data.generation.taskId)"
      @close="closePrompt"
    />
    <CanvasContextMenu
      v-if="contextMenuPosition && auth.status === 'authenticated'"
      :position="contextMenuPosition"
      @close="contextMenuPosition = null"
      @upload="chooseFiles"
      @add-node="addNode"
    />
    <input ref="fileInput" class="local-file-input" type="file" accept="image/*,video/*,audio/mpeg,audio/wav,.mp3,.wav" multiple aria-label="选择本地图片、视频或音频" @change="filesSelected" @cancel="pendingUploadPosition = null" />
    <div v-if="importNotice" class="import-notice" role="alert">
      <span>{{ importNotice }}</span>
      <button type="button" aria-label="关闭提示" @click="importNotice = ''">×</button>
    </div>
  </main>
</template>

<style scoped>
.canvas-header { right: 340px; z-index: 30; }
@media (max-width: 860px) { .canvas-header { top: 70px; right: 24px; } }
@media (max-width: 600px) { .canvas-header { right: 18px; } }
.canvas-header h1 { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.home-button { display: flex; align-items: center; gap: 9px; padding: 0; border: 0; background: none; color: #cad0db; font-size: 13px; cursor: pointer; pointer-events: auto; flex: none; }
.home-button > span:first-child { font-size: 26px; }
.home-button:hover { color: #fff; }
.leave-dialog { max-width: min(440px, calc(100% - 32px)); border: 1px solid #515762; border-radius: 12px; background: #26282b; color: #eeeef0; padding: 24px; }
.leave-dialog::backdrop { background: #000a; }
.leave-dialog h2 { font-size: 18px; font-weight: 500; margin-top: 0; }
.leave-dialog p { color: #bfc5d1; font-size: 14px; line-height: 1.8; }
.leave-dialog > div { display: flex; justify-content: flex-end; gap: 12px; margin-top: 22px; }
.leave-dialog button { padding: 9px 12px; border-radius: 6px; border: 1px solid #535b69; color: #eeeef0; background: #323740; cursor: pointer; }
.restore-overlay button + button { margin-top: 12px; }
.save-controls { position: absolute; z-index: 20; top: 24px; right: 24px; display: flex; align-items: center; gap: 12px; color: #aeb6c5; font-size: 12px; }
.save-controls button,.restore-overlay button { background: #30343c; border: 1px solid #525762; color: #e0e4eb; border-radius: 7px; padding: 7px 12px; cursor: pointer; }
.save-controls button:disabled { opacity: .5; cursor: default; }
.restore-overlay { position: absolute; inset: 0; z-index: 50; display: flex; flex-direction: column; justify-content: center; align-items: center; background: #17191feb; color: #dadfe9; padding: 24px; text-align: center; }
.persistence-notice { position: absolute; z-index: 25; top: 64px; right: 24px; max-width: min(550px, calc(100% - 48px)); padding: 10px 14px; border-radius: 8px; background: #3b2e25; color: #e9cbb6; font-size: 12px; }
@media (max-width: 700px) { .canvas-header { right: 18px; } .save-controls { top: 18px; right: 16px; gap: 8px; } .persistence-notice { top: 112px; } }
.local-file-input { display: none; }
.import-notice { position: absolute; z-index: 30; top: 82px; left: 50%; transform: translateX(-50%); display: flex; align-items: flex-start; gap: 16px; width: max-content; max-width: min(560px, calc(100% - 32px)); padding: 12px 16px; border: 1px solid #65513e; border-radius: 10px; background: #302b26; color: #e9d3b5; font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.import-notice button { flex: none; border: 0; padding: 0 3px; background: transparent; color: inherit; cursor: pointer; font-size: 20px; line-height: 1; }
</style>
