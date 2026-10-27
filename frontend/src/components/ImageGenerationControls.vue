<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import ImageModelSelect from './ImageModelSelect.vue'
import CatalogParameterForm from './CatalogParameterForm.vue'
import { IMAGE_COUNTS, IMAGE_RATIOS, imageModelDefinition, DEFAULT_IMAGE_MODEL, type ImageModelId, type ImageOptions } from '../models/imageModels'
import { parameterSummary, parameterValuesFor, type ModelSelection } from '../models/modelCatalog'
import { catalogModelFor, modelOptionIdFor } from '../models/staticModelCatalog'
import type { NodeProvider } from '../canvas/media'

const props = defineProps<{
  modelValue: ImageOptions
  imageModel?: ImageModelId
  selection?: ModelSelection
  panelPosition: { left: string; top: string; width: string }
}>()
const emit = defineEmits<{
  'update:modelValue': [value: ImageOptions]
  'update:imageModel': [value: ImageModelId]
  'choose-model': [value: { provider: 'ark' | 'apimart'; modelId: string }]
  'update:parameter': [key: string, value: unknown]
}>()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const modelSelect = ref<InstanceType<typeof ImageModelSelect> | null>(null)
const open = ref(false)
const menuId = useId()
const menuStyle = ref({ left: '0px', top: '0px' })

const provider = computed<NodeProvider>(() => (props.selection ? (props.selection.modelId ? 'apimart' : 'none') : 'ark'))
const pickerModelId = computed(() => props.selection ? modelOptionIdFor(props.selection, 'image') : (props.imageModel ?? DEFAULT_IMAGE_MODEL))
const apimartModel = computed(() => (provider.value === 'apimart' ? catalogModelFor(props.selection!.modelId, 'image', props.selection!.parameters) : undefined))
const parameterValues = computed(() => apimartModel.value ? parameterValuesFor(apimartModel.value, props.selection?.parameters ?? {}) : {})
const model = computed(() => imageModelDefinition(props.imageModel ?? DEFAULT_IMAGE_MODEL))
const summary = computed(() => {
  if (provider.value === 'apimart' && apimartModel.value) {
    const text = parameterSummary(apimartModel.value, parameterValues.value)
    return text || '设置参数'
  }
  if (provider.value !== 'ark') return '设置参数'
  return `${props.modelValue.ratio === 'adaptive' ? '自动' : props.modelValue.ratio} · ${props.modelValue.resolution} · ${props.modelValue.count}张`
})
function ratioStyle(ratio: string) {
  const [width = 1, height = 1] = ratio === 'adaptive' ? [4, 3] : ratio.split(':').map(Number)
  const scale = 17 / Math.max(width, height)
  return { width: `${width * scale}px`, height: `${height * scale}px` }
}
function positionMenu() {
  if (!open.value || !trigger.value || !popup.value) return
  const anchor = trigger.value.getBoundingClientRect()
  const box = popup.value.getBoundingClientRect()
  const above = anchor.top - box.height - 8
  menuStyle.value = {
    left: `${Math.max(16, Math.min(anchor.left, window.innerWidth - box.width - 16))}px`,
    top: `${Math.max(16, Math.min(above >= 16 ? above : anchor.bottom + 8, window.innerHeight - box.height - 16))}px`,
  }
}
async function show() {
  if (open.value) { close(); return }
  modelSelect.value?.close()
  open.value = true
  await nextTick()
  positionMenu()
  popup.value?.querySelector<HTMLInputElement>('input')?.focus({ preventScroll: true })
}
function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}
function closeAll() { close(); modelSelect.value?.close() }
function update<K extends keyof ImageOptions>(key: K, value: ImageOptions[K]) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (open.value && event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true) }
  else if (!open.value && event.target === trigger.value && ['ArrowDown', 'ArrowUp'].includes(event.key)) {
    event.preventDefault(); event.stopPropagation(); void show()
  }
}
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) close()
}
function focusout(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && !root.value?.contains(event.relatedTarget)) close()
}
watch(() => props.panelPosition, positionMenu, { flush: 'post' })
// 多源 watch 按元素比较；单选 getter 返回新数组会在每次 props 更新时误触发。
watch([() => props.imageModel, () => pickerModelId.value], () => close())
onMounted(() => document.addEventListener('pointerdown', outside, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside, true))
defineExpose({ close: closeAll })
</script>

<template>
  <div ref="root" class="image-generation-controls" @keydown="keydown" @focusout="focusout">
    <div class="model-select-wrap" @pointerdown="close()" @focusin="close()">
      <ImageModelSelect ref="modelSelect" :model-value="pickerModelId" :provider="provider" :category="'image'" :panel-position="panelPosition" @choose="emit('choose-model', $event)" />
    </div>
    <button ref="trigger" type="button" class="parameter-trigger" :class="{ 'is-open': open }" :aria-label="`图片参数，${summary}`" aria-haspopup="dialog" :aria-expanded="open" :aria-controls="open ? menuId : undefined" @click="show">
      <span v-if="provider === 'ark'" class="summary-symbol" :class="{ 'is-adaptive': modelValue.ratio === 'adaptive' }" :style="ratioStyle(modelValue.ratio)" aria-hidden="true"></span>
      <svg v-else class="summary-sliders" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 7v6m4-9v12m3-7v7m3-10v10m4-8v6" /></svg>
      <span>{{ summary }}</span>
      <svg class="chevron" viewBox="0 0 16 16" aria-hidden="true"><path :d="open ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg>
    </button>
    <div v-if="open" :id="menuId" ref="popup" class="image-parameters" :style="menuStyle" role="dialog" aria-label="图片参数" tabindex="-1" @wheel.stop>
      <template v-if="provider === 'apimart'">
        <CatalogParameterForm v-if="apimartModel" :model="apimartModel" :parameters="parameterValues" @update="(key, value) => emit('update:parameter', key, value)" @resize="positionMenu" />
        <p v-else class="parameter-placeholder" role="status">该历史模型已移出当前列表，原选择与参数继续保留。</p>
      </template>
      <template v-else-if="provider === 'none'">
        <p class="parameter-placeholder" role="status">请先选择模型，再设置对应参数。</p>
      </template>
      <template v-else>
        <fieldset class="parameter-section" aria-label="图片分辨率">
          <legend>分辨率</legend>
          <div class="option-row">
            <label v-for="resolution in model.resolutions" :key="resolution" class="choice compact-choice" :class="{ 'is-selected': modelValue.resolution === resolution }">
              <input type="radio" :name="`${menuId}-resolution`" :value="resolution" :aria-label="resolution" :checked="modelValue.resolution === resolution" @change="update('resolution', resolution)" />
              {{ resolution }}
            </label>
          </div>
        </fieldset>
        <fieldset class="parameter-section" aria-label="图片比例">
          <legend>比例</legend>
          <div class="ratio-grid">
            <label v-for="ratio in IMAGE_RATIOS" :key="ratio" class="choice ratio-choice" :class="{ 'is-selected': modelValue.ratio === ratio }">
              <input type="radio" :name="`${menuId}-ratio`" :value="ratio" :aria-label="ratio === 'adaptive' ? '自动比例' : ratio" :checked="modelValue.ratio === ratio" @change="update('ratio', ratio)" />
              <span class="ratio-icon"><span class="ratio-symbol" :class="{ 'is-adaptive': ratio === 'adaptive' }" :style="ratioStyle(ratio)" aria-hidden="true"></span></span>
              {{ ratio === 'adaptive' ? '自动' : ratio }}
            </label>
          </div>
        </fieldset>
        <fieldset class="parameter-section" aria-label="图片生成数量">
          <legend>生成数量</legend>
          <div class="option-row">
            <label v-for="count in IMAGE_COUNTS" :key="count" class="choice compact-choice" :class="{ 'is-selected': modelValue.count === count }">
              <input type="radio" :name="`${menuId}-count`" :value="count" :aria-label="`${count}张`" :checked="modelValue.count === count" @change="update('count', count)" />
              {{ count }}张
            </label>
          </div>
        </fieldset>
      </template>
    </div>
  </div>
</template>

<style scoped>
.image-generation-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; min-width: 0; }
.model-select-wrap { min-width: 0; }
.parameter-trigger { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 8px; border: 0; border-radius: 8px; color: #e4e4e4; background: transparent; font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; }
.parameter-trigger:hover, .parameter-trigger.is-open { background: #3b3b3b; color: #fff; }
.parameter-trigger:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 2px; }
.chevron { flex: none; width: 10px; height: 10px; fill: none; stroke: #a8a8a8; stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }
.summary-sliders { flex: none; width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 1.4; stroke-linecap: round; stroke-linejoin: round; }
.image-parameters { position: fixed; z-index: 3; width: min(342px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; overscroll-behavior: contain; padding: 12px; border: 1px solid #4b4b4b; border-radius: 14px; background: #262626; color: #e4e4e4; box-shadow: 0 10px 32px #0005; scrollbar-width: thin; scrollbar-color: #555 transparent; }
.parameter-section { min-width: 0; margin: 0 0 12px; padding: 0; border: 0; }
.parameter-section:last-child { margin-bottom: 0; }
.parameter-section legend { margin-bottom: 9px; padding: 0; color: #a8a8a8; font-size: 12px; }
.option-row { display: grid; grid-auto-flow: column; grid-auto-columns: minmax(0, 1fr); gap: 8px; }
.ratio-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 8px; }
.choice { position: relative; display: flex; align-items: center; justify-content: center; border: 1px solid #515151; border-radius: 8px; color: #b5b5b5; font-size: 13px; cursor: pointer; }
.choice:hover { background: #323232; }
.choice.is-selected { border-color: #e4e4e4; background: #3b3b3b; color: #fff; }
.choice input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.choice:has(input:focus-visible) { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.compact-choice { min-height: 32px; }
.ratio-choice { flex-direction: column; gap: 7px; height: 64px; }
.ratio-icon { display: grid; place-items: center; height: 18px; }
.ratio-symbol, .summary-symbol { display: block; flex: none; border: 1px solid currentColor; border-radius: 1px; }
.is-adaptive { border-style: dashed; }
.parameter-placeholder { margin: 2px 0 0; color: #a8a8a8; font-size: 12px; line-height: 1.6; }
</style>
