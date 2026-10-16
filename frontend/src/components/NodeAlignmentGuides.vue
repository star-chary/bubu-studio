<script setup lang="ts">
import type { ViewportTransform } from '@vue-flow/core'
import type { AlignmentGuide } from '../canvas/nodeAlignment'

defineProps<{ guides: AlignmentGuide[]; viewport: ViewportTransform }>()
</script>

<template>
  <!-- 与画布同尺寸的独立屏幕层，线宽 / 延伸长度不随缩放改变，也不拦截拖动。 -->
  <svg v-if="guides.length" class="node-alignment-guides" aria-hidden="true">
    <line v-for="guide in guides" :key="guide.axis" :data-axis="guide.axis"
      :x1="(guide.axis === 'x' ? guide.coordinate : guide.start) * viewport.zoom + viewport.x - (guide.axis === 'y' ? 8 : 0)"
      :y1="(guide.axis === 'y' ? guide.coordinate : guide.start) * viewport.zoom + viewport.y - (guide.axis === 'x' ? 8 : 0)"
      :x2="(guide.axis === 'x' ? guide.coordinate : guide.end) * viewport.zoom + viewport.x + (guide.axis === 'y' ? 8 : 0)"
      :y2="(guide.axis === 'y' ? guide.coordinate : guide.end) * viewport.zoom + viewport.y + (guide.axis === 'x' ? 8 : 0)" />
  </svg>
</template>

<style scoped>
.node-alignment-guides { position: absolute; inset: 0; z-index: 10; width: 100%; height: 100%; overflow: hidden; pointer-events: none; }
.node-alignment-guides line { stroke: #73b7ff; stroke-width: 1; stroke-dasharray: 4 3; }
</style>
