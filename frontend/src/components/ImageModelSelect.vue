<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { IMAGE_MODELS } from '../models/imageModels'
import { VIDEO_MODELS } from '../models/videoModels'
import { catalogModelsFor } from '../models/staticModelCatalog'
import { modelDisplayName } from '../models/modelCatalog'

interface PickOption {
  provider: 'ark' | 'apimart'
  modelId: string
  optionId: string
  name: string
}

const props = defineProps<{ modelValue: string; provider?: 'ark' | 'apimart' | 'none'; category: 'image' | 'video'; panelPosition: { left: string; top: string; width: string } }>()
const emit = defineEmits<{ choose: [value: { provider: 'ark' | 'apimart'; modelId: string }] }>()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const open = ref(false)
const query = ref('')
const focusedIndex = ref(0)
const menuId = useId()
const menuStyle = ref({ left: '0px', top: '0px', maxHeight: 'calc(100dvh - 32px)' })
const listLabel = props.category === 'image' ? '图片模型' : '视频模型'
const triggerLabel = computed(() => `${props.category === 'video' ? '选择视频模型' : '选择模型'}，当前 ${selectedName.value || '未选择'}`)

// 模型定义随前端发布；历史型号仍能回显，但不重新加入新选择列表。
const arkOptions = computed<PickOption[]>(() => {
  const models = props.category === 'image' ? IMAGE_MODELS : VIDEO_MODELS
  return models.filter((model) => !model.id.includes('seedance-2-0-mini')).map((model) => ({
    provider: 'ark' as const, modelId: model.id, optionId: model.id, name: model.name,
  }))
})
const apimartOptions = computed<PickOption[]>(() => catalogModelsFor(props.category).map((model) => ({
  provider: 'apimart' as const, modelId: model.optionId, optionId: model.optionId, name: modelDisplayName(model),
})))
const options = computed(() => [...arkOptions.value, ...apimartOptions.value])
const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return options.value
  return options.value.filter((option) => option.name.toLowerCase().includes(q) || option.modelId.toLowerCase().includes(q))
})
const selected = computed(() => options.value.find((option) => option.optionId === props.modelValue && (!props.provider || option.provider === props.provider)))
const selectedMissing = computed(() => !!props.modelValue && !selected.value)
const selectedName = computed(() => selected.value?.name ?? [...IMAGE_MODELS, ...VIDEO_MODELS].find((model) => model.id === props.modelValue)?.name ?? props.modelValue)
const groups = computed(() => [
  { key: 'ark', label: '方舟' }, { key: 'apimart', label: 'APIMart' },
].map((group) => ({ ...group, options: filtered.value.map((option, index) => ({ ...option, index })).filter((option) => option.provider === group.key) })).filter((group) => group.options.length))

function positionMenu() {
  if (!open.value || !trigger.value || !menu.value) return
  const anchor = trigger.value.getBoundingClientRect()
  const box = menu.value.getBoundingClientRect()
  const above = anchor.top - 24
  const below = window.innerHeight - anchor.bottom - 24
  const available = Math.max(80, Math.min(window.innerHeight - 32, Math.max(above, below)))
  const height = Math.min(menu.value.scrollHeight + 2, available)
  menuStyle.value = {
    left: `${Math.max(16, Math.min(anchor.left, window.innerWidth - box.width - 16))}px`,
    top: `${Math.max(16, Math.min(above >= below ? anchor.top - height - 8 : anchor.bottom + 8, window.innerHeight - height - 16))}px`,
    maxHeight: `${available}px`,
  }
}
function focusOption(index: number) {
  const options = filtered.value
  if (!options.length) return
  focusedIndex.value = (index + options.length) % options.length
  menu.value?.querySelectorAll<HTMLButtonElement>('[role="option"]')[focusedIndex.value]?.focus({ preventScroll: true })
}
async function showMenu() {
  open.value = true
  await nextTick()
  positionMenu()
  query.value = ''
  if (options.value.length > 8) search.value?.focus({ preventScroll: true })
  else focusOption(Math.max(0, filtered.value.findIndex((option) => option.modelId === props.modelValue)))
}
function closeMenu(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}
function choose(option: PickOption) {
  emit('choose', { provider: option.provider, modelId: option.modelId })
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
    const current = event.target === search.value ? (event.key === 'ArrowDown' ? -1 : 0) : focusedIndex.value
    focusOption(event.key === 'Home' ? 0 : event.key === 'End' ? filtered.value.length - 1 : current + (event.key === 'ArrowDown' ? 1 : -1))
  }
}
function closeOnOutside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) closeMenu()
}
function handleFocusOut(event: FocusEvent) {
  if (!(event.relatedTarget instanceof Node) || !root.value?.contains(event.relatedTarget)) closeMenu()
}
watch(() => props.panelPosition, positionMenu, { flush: 'post' })
onMounted(() => document.addEventListener('pointerdown', closeOnOutside, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', closeOnOutside, true))
defineExpose({ close: closeMenu })
</script>

<template>
  <div ref="root" class="model-select" @keydown="handleKeydown" @focusout="handleFocusOut">
    <button
      ref="trigger" type="button" class="model-trigger" :class="{ 'is-open': open, 'is-missing': selectedMissing }"
      :aria-label="triggerLabel" :title="selectedName || '未选择模型'"
      aria-haspopup="listbox" :aria-expanded="open" :aria-controls="open ? menuId : undefined"
      @click="open ? closeMenu() : showMenu()"
    >
      <svg class="model-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M3 7v6m4-9v12m3-7v7m3-10v10m4-8v6" /></svg>
      <span>{{ selectedName || '选择模型' }}</span>
      <svg v-if="selectedMissing" class="missing-mark" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 5v5m0 3v.5" /><circle cx="8" cy="8" r="6.5" /></svg>
      <svg class="model-chevron" viewBox="0 0 16 16" aria-hidden="true"><path :d="open ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg>
    </button>
    <div v-if="open" :id="menuId" ref="menu" class="model-menu" :style="menuStyle" role="listbox" :aria-label="listLabel">
      <div v-if="options.length > 8" class="model-search">
        <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="4.5" /><path d="m10.5 10.5 3 3" /></svg>
        <input ref="search" v-model="query" type="search" :aria-label="`搜索${listLabel}`" placeholder="搜索模型名称或 ID" @input="focusedIndex = 0" />
      </div>
      <template v-for="group in groups" :key="group.key">
        <div class="model-group-heading" :class="{ 'apimart-heading': group.key !== 'ark' }" aria-hidden="true">{{ group.label }}</div>
        <button v-for="option in group.options" :key="`${option.provider}:${option.optionId}`" type="button" class="model-option"
          role="option" :data-provider="option.provider" :data-model-option="option.optionId"
          :aria-label="option.provider === 'apimart' ? `${option.name}（APIMart）` : option.name" :aria-selected="selected?.provider === option.provider && selected?.optionId === option.optionId"
          :tabindex="option.index === focusedIndex ? 0 : -1" @focus="focusedIndex = option.index" @click="choose(option)">
          <span class="model-option-icon"><svg class="model-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M3 7v6m4-9v12m3-7v7m3-10v10m4-8v6" /></svg></span>
          <span class="model-option-copy"><strong>{{ option.name }}</strong></span>
          <svg v-if="selected?.provider === option.provider && selected?.optionId === option.optionId" class="model-check" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 8 3 3 7-7" /></svg>
        </button>
      </template>
      <p v-if="!filtered.length" class="model-catalog-note" role="status">没有找到匹配的模型。</p>
      <p v-if="selectedMissing" class="model-catalog-note is-missing" role="status">历史模型已移出当前列表，原选择与参数继续保留。</p>
    </div>
  </div>
</template>

<style scoped>
.model-select { min-width: 0; }
.model-trigger { display: flex; align-items: center; gap: 7px; max-width: 100%; min-height: 32px; padding: 0 7px; border: 0; border-radius: 8px; background: transparent; color: #e2e2e2; cursor: pointer; font: inherit; font-size: 12px; }
.model-trigger:hover, .model-trigger.is-open { background: #393939; color: #fff; }
.model-trigger.is-missing { color: #e9c9a2; }
.model-trigger > span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
svg { flex: none; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
.model-icon { width: 18px; height: 18px; }
.model-chevron { width: 12px; height: 12px; color: #999; }
.missing-mark { width: 12px; height: 12px; color: #e0a76a; }
.model-menu { position: fixed; z-index: 1; box-sizing: border-box; width: min(346px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; padding: 6px; border: 1px solid #4b4b4b; border-radius: 16px; background: #272727; box-shadow: 0 10px 32px #0005; }
.model-search { display: flex; align-items: center; gap: 7px; margin: 2px 4px 8px; padding: 0 9px; border: 1px solid #4b4b4b; border-radius: 9px; background: #1f1f1f; }
.model-search svg { width: 14px; height: 14px; color: #8f8f8f; }
.model-search input { flex: 1; min-width: 0; padding: 8px 0; border: 0; background: transparent; color: #eee; font: inherit; font-size: 12px; outline: none; }
.model-search input::-webkit-search-cancel-button { display: none; }
.model-group-heading { margin: 6px 8px 4px; color: #8d8d8d; font-size: 11px; }
.model-group-heading.apimart-heading { margin-top: 12px; border-top: 1px solid #3d3d3d; padding-top: 10px; }
.model-option { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 44px; padding: 6px 8px; border: 0; border-radius: 10px; background: transparent; color: #ebebeb; text-align: left; cursor: pointer; font: inherit; }
.model-option + .model-option { margin-top: 3px; }
.model-option[aria-selected="true"] { background: #414141; }
.model-option:hover, .model-option:focus-visible { background: #494949; }
.model-option-icon { display: grid; place-items: center; flex: none; width: 30px; height: 32px; border-radius: 8px; background: #ffffff09; }
.model-option-copy { min-width: 0; }
.model-option-copy strong { font-size: 12px; font-weight: 600; overflow-wrap: anywhere; }
.model-check { width: 15px; height: 15px; margin-left: auto; }
.model-catalog-note { margin: 6px 8px; color: #a7a7a7; font-size: 11.5px; line-height: 1.6; }
.model-catalog-note.is-missing { color: #e0c394; }
</style>
