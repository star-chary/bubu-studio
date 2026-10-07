<script setup lang="ts">
import type { MediaKind } from '../canvas/media'

defineProps<{ kind: MediaKind; count: number; feedback: string }>()
const emit = defineEmits<{ returnToNode: []; close: [] }>()
</script>

<template>
  <section class="reference-selection-bar" aria-label="选择参考" @pointerdown.stop @wheel.stop>
    <svg class="reference-selection-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m20 4-6 16-4-6-6-4L20 4ZM10 14 20 4" /></svg>
    <div class="reference-selection-copy">
      <strong>从画布选择参考</strong>
      <span role="status" aria-live="polite">{{ feedback || `可选${kind === 'image' ? '图片' : '图片、视频和音频'} · 已选 ${count} 项` }}</span>
    </div>
    <button class="reference-return" type="button" @click="emit('returnToNode')">返回节点</button>
    <button class="reference-close" type="button" aria-label="结束参考选择" title="结束选择（Esc），保留已选参考" @click="emit('close')">
      <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="m5 5 10 10M15 5 5 15" /></svg>
    </button>
  </section>
</template>

<style scoped>
.reference-selection-bar { position: fixed; z-index: 25; top: 16px; left: 50%; transform: translateX(-50%); display: flex; align-items: center; gap: 12px; width: max-content; max-width: calc(100% - 32px); padding: 10px 12px; border: 1px solid #5793ff; border-radius: 14px; background: #2469ed; box-shadow: 0 6px 24px #0003; color: #fff; }
.reference-selection-icon { flex: none; width: 32px; height: 32px; padding: 6px; border-radius: 8px; background: #ffffff20; stroke: currentColor; stroke-width: 1.5; stroke-linejoin: round; }
.reference-selection-copy { display: flex; flex-direction: column; gap: 4px; min-width: 0; max-width: 360px; }
.reference-selection-copy strong { font-size: 13px; font-weight: 600; }
.reference-selection-copy span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; color: #e0ebff; }
.reference-selection-bar button { flex: none; min-height: 32px; padding: 0 10px; border: 0; border-radius: 7px; background: #ffffff20; color: #fff; cursor: pointer; font-size: 12px; }
.reference-selection-bar button:hover { background: #ffffff35; }
.reference-selection-bar button:focus-visible { outline-color: #fff; }
.reference-selection-bar .reference-close { display: grid; place-items: center; width: 28px; padding: 0; background: transparent; }
.reference-close svg { width: 18px; height: 18px; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; }
@media (max-width: 540px) { .reference-selection-bar { gap: 8px; } .reference-selection-icon { display: none; } }
</style>
