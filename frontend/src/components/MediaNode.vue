<script setup lang="ts">
import { computed, ref } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import { getPreviewSize, type MediaDimensions, type MediaNodeData } from '../canvas/media'
import type { ReferenceCandidateState } from '../canvas/useNodeReferences'
import { generationStatusLabel } from '../canvas/generationStatus'

const props = defineProps<{ data: MediaNodeData; selected: boolean; referenceState?: ReferenceCandidateState; hasReferences: boolean; isReferenceSource: boolean }>()
const emit = defineEmits<{ loaded: [dimensions: MediaDimensions]; failed: []; retrySave: [] }>()
const size = computed(() => getPreviewSize(props.data))
const videoElement = ref<HTMLMediaElement | null>(null)
const mediaLabel = computed(() => props.data.kind === 'audio' ? '音频' : '视频')
const isPlaying = ref(false)
const currentTime = ref(0)
const duration = ref(0)
const playbackError = ref('')

function formatTime(seconds: number) {
  const total = Math.floor(seconds)
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}

function syncVideoTime() {
  if (!videoElement.value) return
  currentTime.value = videoElement.value.currentTime
  duration.value = Number.isFinite(videoElement.value.duration) ? videoElement.value.duration : 0
}

async function togglePlayback() {
  const video = videoElement.value
  if (!video) return
  playbackError.value = ''
  if (!video.paused) {
    video.pause()
    return
  }
  try {
    await video.play()
  } catch (error) {
    // 快速点击暂停可以中止尚未完成的 play，这不代表文件损坏。
    if (error instanceof DOMException && error.name === 'AbortError') return
    playbackError.value = '暂时无法播放，请再次点击播放按钮。'
  }
}

function seekVideo(event: Event) {
  const video = videoElement.value
  if (!video || !duration.value) return
  video.currentTime = Number((event.currentTarget as HTMLInputElement).value)
  syncVideoTime()
}

function imageLoaded(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  emit('loaded', { width: image.naturalWidth, height: image.naturalHeight })
}

function videoLoaded(event: Event) {
  const video = event.currentTarget as HTMLVideoElement
  syncVideoTime()
  emit('loaded', { width: video.videoWidth, height: video.videoHeight, durationSeconds: duration.value })
}

function audioLoaded() {
  syncVideoTime()
  emit('loaded', { width: 0, height: 0, durationSeconds: duration.value })
}
</script>

<template>
  <article class="media-node" :class="{ 'is-selected': selected, [`reference-${referenceState}`]: referenceState }" :style="{ width: `${size.width}px` }" :aria-label="data.name">
    <Handle v-if="data.kind !== 'audio'" id="reference-in" type="target" :position="Position.Left" :connectable="false" class="reference-handle" :class="{ 'is-visible': hasReferences || referenceState === 'target' }" :style="{ top: `${28 + size.height / 2}px` }" />
    <Handle id="reference-out" type="source" :position="Position.Right" :connectable="false" class="reference-handle" :class="{ 'is-visible': isReferenceSource || referenceState === 'available' || referenceState === 'chosen' }" :style="{ top: `${28 + size.height / 2}px` }" />
    <header class="media-node-header">
      <svg v-if="data.kind === 'image'" viewBox="0 0 20 20" aria-hidden="true">
        <path d="M3 2h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1Zm0 13h14l-4-5-3 3-3-5-4 7Z" fill-rule="evenodd" />
        <circle cx="13" cy="6" r="2" />
      </svg>
      <svg v-else-if="data.kind === 'audio'" viewBox="0 0 20 20" aria-hidden="true"><path d="M8 3v10a3 3 0 1 0 2 3V7l6-2v6a3 3 0 1 0 2 3V0Z" /></svg>
      <svg v-else viewBox="0 0 20 20" aria-hidden="true"><path d="M5 2.5 17 10 5 17.5Z" /></svg>
      <span class="media-node-title" :title="data.name">{{ data.name }}</span>
      <span v-if="data.width && data.height" class="media-node-dimensions">{{ data.width }} × {{ data.height }}</span>
    </header>

    <div class="media-node-preview" :style="{ height: `${size.height}px` }" :aria-busy="data.status === 'loading' || data.generation?.status === 'generating'">
      <template v-if="data.url">
        <img
          v-if="data.kind === 'image'"
          class="media-content"
          :src="data.url"
          :alt="data.name"
          :draggable="false"
          @load="imageLoaded"
          @error="emit('failed')"
          @dragstart.prevent
        />
        <video
          v-else-if="data.kind === 'video'"
          ref="videoElement"
          class="media-content"
          :src="data.url"
          :aria-label="data.name"
          tabindex="-1"
          playsinline
          preload="auto"
          @loadeddata="videoLoaded"
          @error="emit('failed')"
          @play="isPlaying = true"
          @pause="isPlaying = false"
          @ended="isPlaying = false"
          @timeupdate="syncVideoTime"
          @durationchange="syncVideoTime"
        />
        <template v-else>
          <div class="audio-art" aria-hidden="true"><span>♫</span><span>音频素材</span></div>
          <audio ref="videoElement" :src="data.url" :aria-label="data.name" preload="metadata" @loadedmetadata="audioLoaded" @error="emit('failed')" @play="isPlaying = true" @pause="isPlaying = false" @ended="isPlaying = false" @timeupdate="syncVideoTime" @durationchange="syncVideoTime" />
        </template>
        <!-- 画面交给节点拖动，只有独立控件接收播放和进度操作。 -->
        <div v-if="data.kind !== 'image' && data.status === 'ready'" class="video-controls">
          <button
            type="button"
            class="video-play-button nodrag nopan"
            :aria-label="`${isPlaying ? '暂停' : '播放'}${mediaLabel}`"
            :title="isPlaying ? '暂停' : '播放'"
            @click.stop="togglePlayback"
            @keydown.stop
          >
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <path v-if="isPlaying" d="M5 3h4v14H5zm6 0h4v14h-4z" />
              <path v-else d="m6 3 11 7-11 7z" />
            </svg>
          </button>
          <span class="video-time">{{ formatTime(currentTime) }} / {{ formatTime(duration) }}</span>
          <input
            class="video-progress nodrag nopan nowheel"
            type="range"
            :aria-label="`${mediaLabel}进度`"
            min="0"
            :max="duration || 0"
            step="0.01"
            :value="currentTime"
            :disabled="!duration"
            @input="seekVideo"
            @keydown.stop
          />
        </div>
        <p v-if="playbackError" class="video-playback-error" role="alert">{{ playbackError }}</p>
      </template>
      <div v-if="data.status === 'empty'" class="media-placeholder" :aria-label="data.kind === 'image' ? '空图片节点' : '空视频节点'">
        <svg v-if="data.kind === 'image'" viewBox="0 0 64 64" aria-hidden="true">
          <path d="M7 50 25 20a3 3 0 0 1 5 0l12 20 6-10a3 3 0 0 1 5 0l10 20a3 3 0 0 1-3 4H10a3 3 0 0 1-3-4Z" />
          <circle cx="48" cy="17" r="6" />
        </svg>
        <svg v-else viewBox="0 0 64 64" aria-hidden="true"><path d="M19 9a4 4 0 0 1 6-3l29 22a5 5 0 0 1 0 8L25 58a4 4 0 0 1-6-3Z" /></svg>
      </div>
      <div v-else-if="data.status === 'loading'" class="media-loading" role="status">正在读取…</div>
      <p v-else-if="data.status === 'error'" class="media-error" role="alert">{{ data.error }}</p>
      <div v-if="data.generation?.status === 'generating'" class="media-generating" role="status">{{ generationStatusLabel(data.generation, data.kind === 'video' ? '视频' : '图片') }}</div>
      <span v-if="data.storage?.status === 'saving'" class="asset-status" role="status">保存中…</span>
      <div v-else-if="data.storage?.status === 'error'" class="asset-save-error nodrag nopan" @click.stop @pointerdown.stop @keydown.stop>
        <p role="alert">{{ data.storage.error }}</p>
        <button v-if="data.origin === 'upload'" type="button" @click.stop="emit('retrySave')">重新保存</button>
      </div>
      <span v-if="referenceState" class="reference-state-label" aria-hidden="true">{{ referenceState === 'target' ? '正在为此节点选择参考' : referenceState === 'chosen' ? '✓ 已参考' : referenceState === 'available' ? '点击添加参考' : '不可作为参考' }}</span>
    </div>
  </article>
</template>

<style scoped>
.asset-status { position: absolute; top: 8px; right: 8px; padding: 4px 7px; border-radius: 6px; background: #191919c9; color: #c8d6c7; font-size: 11px; pointer-events: none; }
.asset-save-error { position: absolute; left: 10px; right: 10px; bottom: 40px; padding: 8px 10px; border: 1px solid #775d47; border-radius: 8px; background: #28211bef; color: #ecd4b6; font-size: 12px; line-height: 1.6; }
.asset-save-error p { margin: 0; }
.asset-save-error button { margin-top: 5px; padding: 3px 8px; border: 1px solid #91775c; border-radius: 5px; background: #514032; color: #ffe7c7; cursor: pointer; }
.media-node { user-select: none; cursor: grab; }
.media-node:active { cursor: grabbing; }
.media-generating { position: absolute; inset: 0; display: grid; place-items: center; background: #202020df; color: #d7d7d7; font-size: 13px; pointer-events: none; }
.media-node-header { display: flex; align-items: center; gap: 5px; height: 28px; color: #989898; font-size: 13px; }
.media-node-header svg { flex: none; width: 15px; height: 15px; fill: currentColor; }
.media-node-title { min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.media-node-dimensions { flex: none; margin-left: auto; padding-left: 12px; color: #868686; font-variant-numeric: tabular-nums; }
.media-node-preview { position: relative; overflow: hidden; border: 1px solid #363636; border-radius: 11px; background: #262626; }
.is-selected .media-node-preview { border-color: #8c8c8c; }
.reference-available, .reference-chosen { cursor: pointer; }
.reference-unavailable { cursor: not-allowed; }
.reference-unavailable .media-node-preview { opacity: .45; }
.reference-target .media-node-preview, .reference-chosen .media-node-preview, .reference-available:hover .media-node-preview { border-color: #528fff; box-shadow: 0 0 0 1px #528fff; }
.reference-handle { width: 9px; height: 9px; border: 2px solid #111214; border-radius: 50%; background: #7598c7; opacity: 0; pointer-events: none; }
.reference-handle.is-visible { opacity: 1; }
.reference-state-label { position: absolute; top: 10px; left: 10px; max-width: calc(100% - 20px); padding: 5px 9px; border-radius: 6px; background: #171a21db; color: #dbe7ff; font-size: 12px; pointer-events: none; }
.reference-available:not(:hover) .reference-state-label { opacity: 0; }
.media-content { display: block; width: 100%; height: 100%; object-fit: contain; }
.audio-art { display: flex; justify-content: center; align-items: center; gap: 14px; height: 88px; color: #adc0d8; background: linear-gradient(120deg, #283344, #252a32); font-size: 13px; pointer-events: none; }
.audio-art span:first-child { font-size: 38px; }
video.media-content { pointer-events: none; background: #171717; }
.video-controls { position: absolute; inset: auto 0 0; display: flex; align-items: center; flex-wrap: wrap; gap: 0 4px; padding: 16px 10px 10px; background: linear-gradient(transparent, #000b); pointer-events: none; }
.video-play-button { display: flex; align-items: center; justify-content: center; flex: none; width: 32px; height: 32px; border: 0; border-radius: 6px; background: transparent; color: #fff; cursor: pointer; pointer-events: auto; }
.video-play-button:hover { background: #ffffff24; }
.video-play-button svg { width: 17px; height: 17px; fill: currentColor; }
.video-time { color: #fff; font-size: 12px; font-variant-numeric: tabular-nums; }
.video-progress { flex-basis: 100%; min-width: 0; height: 14px; margin: 0; accent-color: #eeeef0; cursor: pointer; pointer-events: auto; }
.video-playback-error { position: absolute; inset: 10px 10px auto; margin: 0; padding: 8px; border-radius: 6px; background: #171717e6; color: #eed4d4; font-size: 12px; }
.media-placeholder, .media-loading { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; pointer-events: none; }
.media-placeholder svg { width: 54px; height: 54px; fill: #555; }
.media-loading { color: #adb1ba; font-size: 13px; background: #262626; }
.media-error { display: flex; align-items: center; justify-content: center; height: 100%; margin: 0; padding: 24px; color: #d5b4b4; font-size: 13px; line-height: 1.8; text-align: center; }
</style>
