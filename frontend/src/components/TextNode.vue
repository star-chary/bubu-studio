<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import { TEXT_NODE_HEIGHT, TEXT_NODE_WIDTH, type TextNodeData } from '../canvas/textNode'
import { MEDIA_NODE_HEADER_HEIGHT } from '../canvas/media'
import type { ReferenceCandidateState } from '../canvas/useNodeReferences'

const props = defineProps<{ data: TextNodeData; selected: boolean; disabled: boolean; referenceState?: ReferenceCandidateState; isReferenceSource: boolean }>()
const emit = defineEmits<{ 'update:text': [value: string]; activate: [] }>()
const text = computed({ get: () => props.data.text ?? '', set: (value: string) => emit('update:text', value) })
</script>

<template>
  <article class="text-node" :class="{ 'is-selected': selected, [`reference-${referenceState}`]: referenceState }" :style="{ width: `${TEXT_NODE_WIDTH}px` }" :aria-label="data.name">
    <Handle id="reference-out" type="source" :position="Position.Right" :connectable="false" class="reference-handle" :class="{ 'is-visible': isReferenceSource || referenceState === 'available' || referenceState === 'chosen' }" :style="{ top: `${MEDIA_NODE_HEADER_HEIGHT + TEXT_NODE_HEIGHT / 2}px` }" />
    <header class="text-node-header" :style="{ height: `${MEDIA_NODE_HEADER_HEIGHT}px` }">
      <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 4h12M10 4v12M7 16h6" /></svg>
      <span :title="data.name">{{ data.name }}</span>
    </header>
    <div class="text-node-body" :style="{ height: `${TEXT_NODE_HEIGHT}px` }">
      <textarea
        class="text-node-input nodrag nopan nowheel"
        v-model="text"
        :aria-label="`${data.name}内容`"
        :disabled="disabled"
        maxlength="10000"
        placeholder="在这里输入文字…"
        spellcheck="false"
        @focus="emit('activate')"
      ></textarea>
      <div v-if="referenceState" class="selection-overlay" aria-hidden="true"><span>{{ referenceState === 'chosen' ? '已选择' : referenceState === 'available' ? '选择文本' : '不可选择' }}</span></div>
    </div>
  </article>
</template>

<style scoped>
.text-node { position: relative; cursor: grab; }
.text-node:active { cursor: grabbing; }
.text-node-header { display: flex; align-items: center; gap: 6px; color: #989898; font-size: 13px; user-select: none; }
.text-node-header svg { flex: none; width: 15px; height: 15px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-width: 1.5; }
.text-node-header span { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.text-node-body { position: relative; overflow: hidden; border: 1px solid #363636; border-radius: 11px; background: #262626; }
.is-selected .text-node-body, .text-node-body:focus-within { border-color: #8c8c8c; }
.reference-available .text-node-body, .reference-chosen .text-node-body { border-color: #528fff; }
.reference-unavailable .text-node-body { opacity: .45; }
.reference-handle { width: 9px; height: 9px; border: 2px solid #111214; border-radius: 50%; background: #7598c7; opacity: 0; pointer-events: none; }
.reference-handle.is-visible { opacity: 1; }
.selection-overlay { position: absolute; inset: 0; cursor: pointer; }
.selection-overlay span { position: absolute; top: 10px; left: 10px; padding: 5px 9px; border-radius: 6px; background: #171a21ed; color: #dbe7ff; font-size: 12px; }
.reference-unavailable .selection-overlay { cursor: not-allowed; }
.text-node-input { display: block; width: 100%; height: 100%; resize: none; padding: 18px; border: 0; outline: 0; background: transparent; color: #eeeef0; font: inherit; font-size: 15px; line-height: 1.65; cursor: text; }
.text-node-input::placeholder { color: #858585; }
.text-node-input:disabled { color: #aaa; cursor: default; }
</style>
