<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { mediaKindLabel, type MediaKind } from '../canvas/media'

const props = defineProps<{ name: string; kind: MediaKind; url: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const player = ref<HTMLMediaElement | null>(null)
const loading = ref(true)
const slow = ref(false)
const failed = ref(false)
let slowTimer: ReturnType<typeof setTimeout> | undefined

function loaded() { loading.value = false; clearTimeout(slowTimer) }
function fail() { loaded(); failed.value = true }
onMounted(() => {
  dialog.value?.showModal()
  slowTimer = setTimeout(() => { slow.value = true }, 30_000)
})
onBeforeUnmount(() => {
  clearTimeout(slowTimer)
  if (player.value) {
    player.value.pause()
    player.value.removeAttribute('src')
    player.value.load()
  }
  dialog.value?.close()
})
</script>

<template>
  <dialog ref="dialog" class="media-preview-dialog" data-media-preview :aria-label="`${mediaKindLabel[props.kind]}全屏预览`" @cancel.prevent="emit('close')" @keydown.stop @pointerdown.stop @wheel.stop>
    <header class="media-preview-header">
      <span class="media-preview-name" :title="name">{{ name }}</span>
      <button type="button" aria-label="关闭全屏预览" title="关闭（Esc）" autofocus @click="emit('close')">
        <svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg>
      </button>
    </header>
    <div class="media-preview-body" :aria-busy="loading">
      <img v-if="kind === 'image'" :src="url" :alt="name" :draggable="false" @load="loaded" @error="fail" />
      <video v-else-if="kind === 'video'" ref="player" :src="url" :aria-label="name" controls playsinline preload="auto" @loadeddata="loaded" @error="fail" />
      <audio v-else ref="player" :src="url" :aria-label="name" controls preload="auto" @loadeddata="loaded" @error="fail" />
      <p v-if="failed" class="media-preview-status has-error" role="alert">预览加载失败，请关闭后重试。</p>
      <p v-else-if="loading" class="media-preview-status" role="status">{{ slow ? '预览加载已超过 30 秒，仍在读取素材…' : '正在加载预览…' }}</p>
    </div>
  </dialog>
</template>

<style scoped>
.media-preview-dialog { position: fixed; inset: 0; width: 100vw; height: 100dvh; max-width: none; max-height: none; margin: 0; padding: 0; border: 0; color: #e9e9ed; background: #111214; overflow: hidden; }
.media-preview-dialog[open] { display: flex; flex-direction: column; }
.media-preview-dialog::backdrop { background: #111214; }
.media-preview-header { flex: none; display: flex; align-items: center; justify-content: space-between; gap: 24px; height: 64px; padding: 12px 24px; border-bottom: 1px solid #ffffff12; }
.media-preview-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.media-preview-header button { display: grid; place-items: center; flex: none; width: 38px; height: 38px; padding: 0; border: 0; border-radius: 8px; color: #e9e9ed; background: #ffffff0c; cursor: pointer; }
.media-preview-header button:hover { background: #ffffff1a; }
.media-preview-header button:focus-visible { outline: 2px solid #8aafea; outline-offset: 3px; }
.media-preview-header svg { width: 22px; height: 22px; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; }
.media-preview-body { position: relative; display: flex; align-items: center; justify-content: center; flex: 1; min-height: 0; padding: 16px; }
.media-preview-body img, .media-preview-body video { display: block; width: 100%; height: 100%; object-fit: contain; }
.media-preview-body audio { width: min(600px, 100%); }
.media-preview-status { position: absolute; top: 24px; left: 50%; transform: translateX(-50%); max-width: calc(100% - 32px); margin: 0; padding: 10px 16px; border-radius: 8px; background: #242427ef; color: #bbbcc4; font-size: 13px; text-align: center; pointer-events: none; }
.media-preview-status.has-error { color: #edc4c4; }
@media (max-width: 600px) { .media-preview-header { padding: 12px 16px; } .media-preview-body { padding: 8px; } }
</style>
