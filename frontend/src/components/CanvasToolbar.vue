<script setup lang="ts">
import { MAX_ZOOM, MIN_ZOOM } from '../canvas/viewport'

defineProps<{ zoom: number }>()
defineEmits<{ zoomIn: []; zoomOut: []; reset: [] }>()
</script>

<template>
  <div class="canvas-toolbar" role="group" aria-label="画布视图控制">
    <button
      type="button"
      class="icon-button"
      aria-label="缩小画布"
      title="缩小画布"
      :disabled="zoom <= MIN_ZOOM"
      @click="$emit('zoomOut')"
    >
      <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 10h12" /></svg>
    </button>
    <output class="zoom-value" aria-label="当前缩放比例">{{ Math.round(zoom * 100) }}%</output>
    <button
      type="button"
      class="icon-button"
      aria-label="放大画布"
      title="放大画布"
      :disabled="zoom >= MAX_ZOOM"
      @click="$emit('zoomIn')"
    >
      <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 10h12M10 4v12" /></svg>
    </button>
    <span class="toolbar-divider" aria-hidden="true"></span>
    <button type="button" class="reset-button" title="恢复初始位置和 100% 缩放" @click="$emit('reset')">
      <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 7a6 6 0 1 1-1 6M5 3v4h4" /></svg>
      <span>重置视图</span>
    </button>
  </div>
</template>
