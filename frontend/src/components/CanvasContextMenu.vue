<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { MediaKind } from '../canvas/media'

const props = defineProps<{ position: { x: number; y: number }; target?: 'canvas' | 'node'; canPaste?: boolean }>()
const emit = defineEmits<{ close: []; upload: []; addNode: [kind: Exclude<MediaKind, 'audio'>]; deleteNode: []; copyNode: [keepReferences: boolean]; paste: [] }>()

const panel = ref<HTMLDivElement | null>(null)
const screen = ref<'main' | 'nodes'>('main')
const placement = ref({ ...props.position })
const hint = ref<{ kind: 'single' | 'references'; x: number; y: number } | null>(null)

function showHint(event: Event, kind: 'single' | 'references') {
  const current = event.currentTarget
  if (!(current instanceof HTMLElement)) return
  const anchor = current.matches('.copy-help') ? current : current.querySelector<HTMLElement>('.copy-help')
  if (!anchor) return
  const box = anchor.getBoundingClientRect()
  const width = 250
  const right = box.right + 8
  hint.value = { kind, x: Math.max(8, Math.min(right + width <= window.innerWidth - 8 ? right : box.left - width - 8, window.innerWidth - width - 8)), y: Math.max(8, Math.min(box.top - 4, window.innerHeight - 48)) }
}

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
watch([() => props.position, () => props.target, screen], async () => {
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
  const items = Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])
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
    :aria-label="target === 'node' ? '节点菜单' : screen === 'main' ? '画布菜单' : '添加节点'"
    :style="{ left: `${placement.x}px`, top: `${placement.y}px` }"
    tabindex="-1"
    @keydown="handleKeydown"
    @contextmenu.prevent
    @pointerdown.stop
    @mousedown.stop
    @wheel.stop.prevent
  >
    <template v-if="target === 'node'">
      <button type="button" role="menuitem" tabindex="-1" aria-describedby="copy-single-hint" @click="emit('copyNode', false)" @focus="showHint($event, 'single')" @blur="hint = null">
        <svg viewBox="0 0 20 20" aria-hidden="true"><rect x="6" y="6" width="10" height="11" rx="1.5" /><path d="M13 6V4a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h2" /></svg>
        复制
        <span class="copy-help" aria-hidden="true" @mouseenter="showHint($event, 'single')" @mouseleave="hint = null"><svg viewBox="0 0 20 20"><circle cx="10" cy="10" r="8" /><path d="M7.7 7.4a2.4 2.4 0 1 1 3.8 2c-1 .7-1.5 1.1-1.5 2.3M10 14.5h.01" /></svg></span>
      </button>
      <button type="button" role="menuitem" tabindex="-1" aria-describedby="copy-references-hint" @click="emit('copyNode', true)" @focus="showHint($event, 'references')" @blur="hint = null">
        <svg viewBox="0 0 20 20" aria-hidden="true"><rect x="6" y="6" width="10" height="11" rx="1.5" /><path d="M13 6V4a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h2" /></svg>
        复制副本
        <span class="copy-help" aria-hidden="true" @mouseenter="showHint($event, 'references')" @mouseleave="hint = null"><svg viewBox="0 0 20 20"><circle cx="10" cy="10" r="8" /><path d="M7.7 7.4a2.4 2.4 0 1 1 3.8 2c-1 .7-1.5 1.1-1.5 2.3M10 14.5h.01" /></svg></span>
      </button>
      <button type="button" role="menuitem" tabindex="-1" aria-keyshortcuts="Delete" @click="emit('deleteNode')">
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <path d="M3 5h14M7 5V3h6v2M5 5l1 12h8l1-12M8 8v6m4-6v6" />
        </svg>
        删除
        <kbd aria-hidden="true">Delete</kbd>
      </button>
    </template>
    <template v-else-if="screen === 'main'">
      <button type="button" role="menuitem" tabindex="-1" @click="emit('upload')">上传</button>
      <button type="button" role="menuitem" tabindex="-1" @click="screen = 'nodes'">添加节点</button>
      <button type="button" role="menuitem" tabindex="-1" :disabled="!canPaste" @click="emit('paste')">粘贴</button>
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
  <Teleport to="body">
    <div v-show="hint?.kind === 'single'" id="copy-single-hint" class="copy-tooltip" role="tooltip" :style="{ left: `${hint?.x ?? 0}px`, top: `${hint?.y ?? 0}px` }">仅复制节点本身</div>
    <div v-show="hint?.kind === 'references'" id="copy-references-hint" class="copy-tooltip" role="tooltip" :style="{ left: `${hint?.x ?? 0}px`, top: `${hint?.y ?? 0}px` }">复制节点并保留对原参考节点的连接</div>
  </Teleport>
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

.canvas-context-menu button:disabled { color: #777; cursor: not-allowed; background: transparent; }
.copy-help { display: inline-flex; align-items: center; color: #a7a7aa; }
.canvas-context-menu .copy-help svg { width: 15px; height: 15px; }
.copy-tooltip { position: fixed; z-index: 60; width: max-content; max-width: min(250px, calc(100vw - 16px)); padding: 7px 10px; border: 1px solid #4b4b4b; border-radius: 7px; background: #1e1e1e; color: #eeeef0; font-size: 12px; line-height: 1.4; box-shadow: 0 6px 20px #0005; pointer-events: none; }

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

.canvas-context-menu kbd {
  margin-left: auto;
  color: #919191;
  font: inherit;
  font-size: 12px;
  font-weight: 400;
}
</style>
