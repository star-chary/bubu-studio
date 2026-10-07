<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { IMAGE_MODELS, type ImageModelId } from '../models/imageModels'

const props = defineProps<{ modelValue: ImageModelId; panelPosition: { left: string; top: string } }>()
const emit = defineEmits<{ 'update:modelValue': [value: ImageModelId] }>()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const focusedIndex = ref(0)
const menuId = useId()
const menuStyle = ref({ left: '0px', top: '0px' })
const selected = computed(() => IMAGE_MODELS.find((model) => model.id === props.modelValue) ?? IMAGE_MODELS[0])

function positionMenu() {
  if (!open.value || !trigger.value || !menu.value) return
  const anchor = trigger.value.getBoundingClientRect()
  const box = menu.value.getBoundingClientRect()
  const above = anchor.top - box.height - 8
  menuStyle.value = {
    left: `${Math.max(16, Math.min(anchor.left, window.innerWidth - box.width - 16))}px`,
    top: `${Math.max(16, Math.min(above >= 16 ? above : anchor.bottom + 8, window.innerHeight - box.height - 16))}px`,
  }
}

function focusOption(index: number) {
  focusedIndex.value = (index + IMAGE_MODELS.length) % IMAGE_MODELS.length
  menu.value?.querySelectorAll<HTMLButtonElement>('[role="option"]')[focusedIndex.value]?.focus({ preventScroll: true })
}

async function showMenu() {
  open.value = true
  await nextTick()
  positionMenu()
  focusOption(IMAGE_MODELS.findIndex((model) => model.id === props.modelValue))
}

function closeMenu(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}

function choose(model: ImageModelId) {
  emit('update:modelValue', model)
  closeMenu(true)
}

function handleKeydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (!open.value) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      event.stopPropagation()
      void showMenu()
    }
    return
  }
  // 列表打开时 Esc 只收起列表，不关闭整个提示词面板。
  event.stopPropagation()
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu(true)
  } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
    event.preventDefault()
    focusOption(event.key === 'Home' ? 0 : event.key === 'End' ? IMAGE_MODELS.length - 1 : focusedIndex.value + (event.key === 'ArrowDown' ? 1 : -1))
  }
}

function closeOnOutside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) closeMenu()
}

function handleFocusOut(event: FocusEvent) {
  if (!(event.relatedTarget instanceof Node) || !root.value?.contains(event.relatedTarget)) closeMenu()
}

// 等父面板的新位置写入 DOM 后再定位；窗口变化或生成预览改变节点尺寸时都能跟随。
watch(() => props.panelPosition, positionMenu, { flush: 'post' })

onMounted(() => {
  document.addEventListener('pointerdown', closeOnOutside, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', closeOnOutside, true)
})
</script>

<template>
  <div ref="root" class="image-model-select" @keydown="handleKeydown" @focusout="handleFocusOut">
    <button
      ref="trigger" type="button" class="model-trigger" :class="{ 'is-open': open }"
      :aria-label="`选择模型，当前 ${selected.name}`" :title="selected.name"
      aria-haspopup="listbox" :aria-expanded="open" :aria-controls="open ? menuId : undefined"
      @click="open ? closeMenu() : showMenu()"
    >
      <svg class="model-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M3 7v6m4-9v12m3-7v7m3-10v10m4-8v6" /></svg>
      <span>{{ selected.name }}</span>
      <svg class="model-chevron" viewBox="0 0 16 16" aria-hidden="true"><path :d="open ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg>
    </button>
    <div v-if="open" :id="menuId" ref="menu" class="model-menu" :style="menuStyle" role="listbox" aria-label="图片模型">
      <button
        v-for="(model, index) in IMAGE_MODELS" :key="model.id" type="button" class="model-option"
        role="option" :aria-label="model.name" :aria-selected="model.id === modelValue"
        :tabindex="index === focusedIndex ? 0 : -1" @focus="focusedIndex = index" @click="choose(model.id)"
      >
        <span class="model-option-icon"><svg class="model-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M3 7v6m4-9v12m3-7v7m3-10v10m4-8v6" /></svg></span>
        <span class="model-option-copy"><strong>{{ model.name }}</strong><span>文生图 · 2K · 单张</span></span>
        <svg v-if="model.id === modelValue" class="model-check" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 8 3 3 7-7" /></svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.image-model-select { min-width: 0; }
.model-trigger { display: flex; align-items: center; gap: 7px; max-width: 100%; min-height: 32px; padding: 0 7px; border: 0; border-radius: 8px; background: transparent; color: #e2e2e2; cursor: pointer; font: inherit; font-size: 12px; }
.model-trigger:hover, .model-trigger.is-open { background: #393939; color: #fff; }
.model-trigger > span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
svg { flex: none; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
.model-icon { width: 18px; height: 18px; }
.model-chevron { width: 12px; height: 12px; color: #999; }
.model-menu { position: fixed; z-index: 1; width: min(346px, calc(100vw - 32px)); max-height: calc(100vh - 32px); overflow-y: auto; padding: 6px; border: 1px solid #4b4b4b; border-radius: 16px; background: #272727; box-shadow: 0 10px 32px #0005; }
.model-option { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 58px; padding: 8px; border: 0; border-radius: 10px; background: transparent; color: #ebebeb; text-align: left; cursor: pointer; font: inherit; }
.model-option + .model-option { margin-top: 3px; }
.model-option[aria-selected="true"] { background: #414141; }
.model-option:hover, .model-option:focus-visible { background: #494949; }
.model-option-icon { display: grid; place-items: center; flex: none; width: 30px; height: 32px; border-radius: 8px; background: #ffffff09; }
.model-option-copy { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.model-option-copy strong { font-size: 12px; font-weight: 600; overflow-wrap: anywhere; }
.model-option-copy > span { color: #aaa; font-size: 11px; }
.model-check { width: 15px; height: 15px; margin-left: auto; }
</style>
