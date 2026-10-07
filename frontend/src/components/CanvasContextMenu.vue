<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { MediaKind } from '../canvas/media'

const props = defineProps<{ position: { x: number; y: number } }>()
const emit = defineEmits<{ close: []; upload: []; addNode: [kind: Exclude<MediaKind, 'audio'>] }>()

const panel = ref<HTMLDivElement | null>(null)
const screen = ref<'main' | 'nodes'>('main')
const placement = ref({ ...props.position })

async function placePanel() {
  await nextTick()
  if (!panel.value) return
  const bounds = panel.value.getBoundingClientRect()
  // 菜单使用屏幕坐标，不受画布平移和缩放影响。
  placement.value = {
    x: Math.max(8, Math.min(props.position.x, window.innerWidth - bounds.width - 8)),
    y: Math.max(8, Math.min(props.position.y, window.innerHeight - bounds.height - 8)),
  }
}

watch(() => props.position, () => { screen.value = 'main' }, { flush: 'sync' })
watch([() => props.position, screen], async () => {
  await placePanel()
  panel.value?.focus({ preventScroll: true })
}, { immediate: true })

function closeOnOutside(event: PointerEvent) {
  if (event.target instanceof Node && !panel.value?.contains(event.target)) emit('close')
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' || event.key === 'Tab') {
    if (event.key === 'Escape') event.preventDefault()
    emit('close')
    return
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const items = Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
  const current = items.findIndex((item) => item === document.activeElement)
  const index = event.key === 'Home' ? 0
    : event.key === 'End' ? items.length - 1
    : event.key === 'ArrowDown' ? (current + 1) % items.length
    : (current <= 0 ? items.length : current) - 1
  items[index]?.focus()
}

onMounted(() => {
  document.addEventListener('pointerdown', closeOnOutside, true)
  window.addEventListener('resize', placePanel)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', closeOnOutside, true)
  window.removeEventListener('resize', placePanel)
})
</script>

<template>
  <div
    ref="panel"
    class="canvas-context-menu"
    role="menu"
    :aria-label="screen === 'main' ? '画布菜单' : '添加节点'"
    :style="{ left: `${placement.x}px`, top: `${placement.y}px` }"
    tabindex="-1"
    @keydown="handleKeydown"
    @contextmenu.prevent
    @pointerdown.stop
    @mousedown.stop
    @wheel.stop.prevent
  >
    <template v-if="screen === 'main'">
      <button type="button" role="menuitem" tabindex="-1" @click="emit('upload')">上传</button>
      <button type="button" role="menuitem" tabindex="-1" @click="screen = 'nodes'">添加节点</button>
    </template>
    <template v-else>
      <div class="context-menu-title">添加节点</div>
      <button type="button" role="menuitem" tabindex="-1" @click="emit('addNode', 'image')">
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <rect x="3" y="3" width="14" height="14" rx="2" />
          <path d="m3 12 4-4 6 6 2-2 2 2" />
          <circle cx="13" cy="7" r="1" />
        </svg>
        图片
      </button>
      <button type="button" role="menuitem" tabindex="-1" @click="emit('addNode', 'video')">
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <rect x="3" y="3" width="14" height="14" rx="2" />
          <path d="m8 7 5 3-5 3z" />
        </svg>
        视频
      </button>
    </template>
  </div>
</template>

<style scoped>
.canvas-context-menu {
  position: fixed;
  z-index: 20;
  width: 220px;
  max-width: calc(100vw - 16px);
  max-height: calc(100dvh - 16px);
  overflow-y: auto;
  padding: 8px;
  border: 1px solid #383838;
  border-radius: 16px;
  background: #262626;
  box-shadow: 0 8px 28px #0003;
  outline: none;
  user-select: none;
}

.context-menu-title {
  padding: 8px 7px 9px;
  color: #919191;
  font-size: 13px;
  line-height: 20px;
  font-weight: 600;
}

.canvas-context-menu button {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  height: 36px;
  padding: 0 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #eeeef0;
  text-align: left;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}

.canvas-context-menu button:hover,
.canvas-context-menu button:focus-visible {
  background: #373737;
  outline: none;
}

.canvas-context-menu svg {
  flex: none;
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
</style>
