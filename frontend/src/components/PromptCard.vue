<script setup lang="ts">
import { ref } from 'vue'
import type { LibraryPrompt } from '../api/promptLibrary'

defineProps<{ prompt: LibraryPrompt }>()
const strip = ref<HTMLElement | null>(null)
const dialog = ref<HTMLDialogElement | null>(null)
const current = ref(0), expanded = ref(false), copyStatus = ref(''), previewIndex = ref(0)
function openPreview(index: number) { previewIndex.value = index; dialog.value?.showModal() }
function scrollImages(direction: number) {
  const root = strip.value
  const first = root?.firstElementChild as HTMLElement | null
  if (root && first) root.scrollBy({ left: direction * (first.clientWidth + 5), behavior: 'smooth' })
}
function onScroll() {
  const root = strip.value
  const first = root?.firstElementChild as HTMLElement | null
  if (root && first) current.value = Math.round(root.scrollLeft / (first.clientWidth + 5))
}
async function copy(content: string) {
  try { await navigator.clipboard.writeText(content); copyStatus.value = '已复制' }
  catch { copyStatus.value = '复制失败，请手动选择文字复制' }
}
</script>

<template>
  <article class="prompt-card">
    <div v-if="prompt.images.length" class="gallery">
      <div ref="strip" class="image-strip" tabindex="0" :aria-label="`${prompt.title} 的图片，可左右滑动`" @scroll="onScroll" @keydown.left.prevent="scrollImages(-1)" @keydown.right.prevent="scrollImages(1)">
        <div v-for="(picture, index) in prompt.images" :key="picture.id" class="image-frame"><button type="button" class="preview-image" :aria-label="`查看${prompt.title}第 ${index + 1} 张大图`" @click="openPreview(index)"><img :src="picture.url" :alt="`${prompt.title}，第 ${index + 1} 张`" loading="lazy" /></button></div>
      </div>
      <span class="image-count">{{ Math.min(current + 1, prompt.images.length) }} / {{ prompt.images.length }}</span>
      <button v-if="prompt.images.length > 1 && current > 0" type="button" class="image-arrow previous" aria-label="上一张图片" @click="scrollImages(-1)">‹</button>
      <button v-if="prompt.images.length > 1 && current < prompt.images.length - 1" type="button" class="image-arrow next" aria-label="下一张图片" @click="scrollImages(1)">›</button>
    </div>
    <div v-else class="image-empty">暂无图片</div>
    <div class="card-body">
      <h3>{{ prompt.title }}</h3>
      <p class="prompt-text" :class="{ expanded: expanded || prompt.content.length <= 120 }">{{ prompt.content }}</p>
      <div class="card-actions"><button v-if="prompt.content.length > 120" type="button" class="expand" @click="expanded = !expanded">{{ expanded ? '收起' : '展开全文' }}</button><span class="copy-status" role="status">{{ copyStatus }}</span><button type="button" class="copy" @click="copy(prompt.content)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></svg>复制提示词</button></div>
    </div>
    <dialog ref="dialog" class="image-dialog" :aria-label="`${prompt.title}大图`" @click.self="dialog?.close()" @keydown.left.prevent="previewIndex = Math.max(0, previewIndex - 1)" @keydown.right.prevent="previewIndex = Math.min(prompt.images.length - 1, previewIndex + 1)">
      <button type="button" class="close-preview" aria-label="关闭大图" @click="dialog?.close()">×</button>
      <img v-if="prompt.images[previewIndex]" :src="prompt.images[previewIndex].url" :alt="`${prompt.title}，第 ${previewIndex + 1} 张`" />
      <div class="preview-controls"><button type="button" :disabled="previewIndex === 0" @click="previewIndex--">上一张</button><span>{{ previewIndex + 1 }} / {{ prompt.images.length }}</span><button type="button" :disabled="previewIndex >= prompt.images.length - 1" @click="previewIndex++">下一张</button></div>
    </dialog>
  </article>
</template>

<style scoped>
.prompt-card { overflow: hidden; border: 1px solid #424242; border-radius: 13px; background: #2b2b2b; }
.gallery { position: relative; background: #1a1a1a; }
.image-strip { display: flex; gap: 5px; overflow-x: auto; overscroll-behavior-x: contain; scroll-snap-type: x mandatory; scrollbar-width: none; }
.image-strip::-webkit-scrollbar { display: none; }
.image-frame { flex: 0 0 72%; height: clamp(190px, 19vw, 285px); overflow: hidden; scroll-snap-align: start; background: #333; }
.image-frame:only-child { flex-basis: 100%; }
.preview-image { display: block; width: 100%; height: 100%; padding: 0; border: 0; background: transparent; cursor: zoom-in; }
.image-frame img { display: block; width: 100%; height: 100%; object-fit: cover; }
.image-count { position: absolute; left: 12px; bottom: 12px; padding: 5px 8px; border-radius: 6px; background: #111b; color: #fff; font-size: 11px; pointer-events: none; }
.image-arrow { position: absolute; top: 50%; display: grid; place-items: center; width: 34px; height: 34px; padding: 0 0 3px; transform: translateY(-50%); border: 1px solid #ffffff40; border-radius: 50%; background: #1a1a1acb; color: white; font-size: 26px; line-height: 1; cursor: pointer; }
.image-arrow:hover { background: #111; }.previous { left: 10px; }.next { right: 10px; }
.image-empty { display: grid; place-items: center; height: 210px; background: #252525; color: #888; font-size: 13px; }
.card-body { padding: 20px 20px 17px; }
h3 { margin: 0 0 10px; color: #eee; font-size: 16px; font-weight: 600; overflow-wrap: anywhere; }
.prompt-text { display: -webkit-box; min-height: 44px; margin: 0; overflow: hidden; color: #aaa; font-size: 13px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.prompt-text.expanded { display: block; }
.card-actions { display: flex; align-items: center; gap: 8px; min-height: 36px; margin-top: 16px; }
.expand { padding: 5px 0; border: 0; background: none; color: #bbb; cursor: pointer; font-size: 12px; }
.copy-status { margin-left: auto; color: #aaa; font-size: 12px; }
.copy { display: inline-flex; align-items: center; gap: 7px; padding: 8px 11px; border: 1px solid #555; border-radius: 8px; background: #343434; color: #eee; font-size: 12px; cursor: pointer; white-space: nowrap; }
.copy:hover { background: #454545; }.copy svg { width: 15px; height: 15px; }
.image-dialog { position: fixed; width: min(1200px, 95vw); height: min(860px, 90dvh); padding: 54px 22px 66px; border: 1px solid #484848; border-radius: 12px; background: #1c1c1c; color: #eee; }
.image-dialog::backdrop { background: #000c; }
.image-dialog > img { width: 100%; height: 100%; object-fit: contain; }
.close-preview { position: absolute; top: 10px; right: 16px; width: 35px; height: 35px; border: 0; background: transparent; color: #eee; font-size: 27px; cursor: pointer; }
.preview-controls { position: absolute; right: 0; bottom: 17px; left: 0; display: flex; align-items: center; justify-content: center; gap: 20px; font-size: 12px; }
.preview-controls button { padding: 7px 12px; border: 1px solid #555; border-radius: 7px; background: #333; color: #eee; cursor: pointer; }
@media (max-width: 600px) { .image-frame { height: 220px; } .card-body { padding: 17px; } }
@media (prefers-reduced-motion: reduce) { .image-strip { scroll-behavior: auto; } }
</style>
