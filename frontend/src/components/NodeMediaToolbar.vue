<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { MediaNodeData } from '../canvas/media'
import { downloadMedia, mediaSource } from '../canvas/mediaActions'

const props = defineProps<{ data: MediaNodeData; anchor: { x: number; y: number; width: number } }>()
const emit = defineEmits<{ preview: [] }>()
const available = computed(() => !!mediaSource(props.data))
const downloading = ref(false)
const error = ref('')
const windowWidth = ref(window.innerWidth)
let controller: AbortController | undefined
const layout = computed(() => ({
  left: `${Math.max(8, Math.min(props.anchor.x + props.anchor.width / 2 - 84, windowWidth.value - 176))}px`,
  top: `${Math.max(8, props.anchor.y - 50)}px`,
}))

async function download() {
  if (!available.value || downloading.value) return
  const request = new AbortController()
  controller = request
  downloading.value = true
  error.value = ''
  try {
    await downloadMedia({ ...props.data }, request.signal)
  } catch (cause) {
    if (!request.signal.aborted) error.value = cause instanceof Error && !(cause instanceof TypeError) ? cause.message : '素材下载失败，请检查网络后重试。'
  } finally {
    if (controller === request) downloading.value = false
  }
}

watch(() => mediaSource(props.data), () => {
  controller?.abort()
  controller = undefined
  downloading.value = false
  error.value = ''
})
function resize() { windowWidth.value = window.innerWidth }
onMounted(() => window.addEventListener('resize', resize))
onBeforeUnmount(() => { controller?.abort(); window.removeEventListener('resize', resize) })
</script>

<template>
  <div class="node-media-tools" data-node-tools :style="layout" @pointerdown.stop @mousedown.stop @click.stop @wheel.stop @keydown.stop>
    <div class="node-media-toolbar" role="toolbar" :aria-label="`${data.name}的素材工具`">
      <button type="button" aria-label="下载素材" :disabled="!available || downloading" :aria-busy="downloading" :title="available ? '下载原始素材' : '当前节点还没有素材'" @click="download">
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M10 2v10m-4-4 4 4 4-4M3 13v4h14v-4" /></svg>
        {{ downloading ? '下载中' : '下载' }}
      </button>
      <span class="tool-divider" aria-hidden="true"></span>
      <button type="button" aria-label="全屏预览" :disabled="!available" :title="available ? '全屏预览素材' : '当前节点还没有素材'" @click="emit('preview')">
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M12 3h5v5m0-5-6 6M8 17H3v-5m0 5 6-6" /></svg>
        放大
      </button>
    </div>
    <p v-if="error" class="download-error" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.node-media-tools { position: fixed; z-index: 40; width: 168px; }
.node-media-toolbar { display: flex; align-items: center; height: 42px; padding: 4px; border: 1px solid #3c3c3e; border-radius: 9px; background: #262626; box-shadow: 0 5px 18px #0005; }
button { display: flex; align-items: center; justify-content: center; flex: 1; gap: 7px; height: 32px; padding: 0; border: 0; border-radius: 5px; color: #e5e5e7; background: transparent; font: inherit; font-size: 13px; cursor: pointer; }
button:hover:not(:disabled) { background: #ffffff13; }
button:focus-visible { outline: 2px solid #8aafea; outline-offset: -2px; }
button:disabled { color: #747479; cursor: default; }
svg { width: 17px; height: 17px; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }
.tool-divider { width: 1px; height: 16px; margin: 0 3px; background: #454548; }
.download-error { width: max-content; max-width: min(300px, calc(100vw - 32px)); margin: 6px 0 0; padding: 8px 10px; border-radius: 6px; background: #302729; color: #edc4c4; font-size: 12px; line-height: 1.6; }
</style>
