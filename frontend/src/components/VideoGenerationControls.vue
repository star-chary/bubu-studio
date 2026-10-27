<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import ImageModelSelect from './ImageModelSelect.vue'
import CatalogParameterForm from './CatalogParameterForm.vue'
import { VIDEO_MODEL, videoModelDefinition, type VideoMode, type VideoModelId, type VideoOptions } from '../models/videoModels'
import { parameterSummary, parameterValuesFor, type ModelSelection } from '../models/modelCatalog'
import { catalogModelFor, modelOptionIdFor } from '../models/staticModelCatalog'
import { VIDEO_MODES, apimartVideoModes, compatibleVideoMode } from '../models/apimartVideoDefinitions'
import type { NodeProvider } from '../canvas/media'

const props = defineProps<{
  modelValue: VideoOptions
  videoModel?: VideoModelId
  videoMode?: VideoMode
  selection?: ModelSelection
  referenceCount: number
  referenceError: string
  panelPosition: { left: string; top: string; width: string }
}>()
const emit = defineEmits<{
  'update:modelValue': [value: VideoOptions]
  'update:videoModel': [value: VideoModelId]
  'update:videoMode': [value: VideoMode]
  'choose-model': [value: { provider: 'ark' | 'apimart'; modelId: string }]
  'update:parameter': [key: string, value: unknown]
}>()
type Menu = 'model' | 'mode' | 'parameters'
const root = ref<HTMLElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const modelSelect = ref<InstanceType<typeof ImageModelSelect> | null>(null)
const open = ref<Menu | null>(null)
const menuId = useId()
const menuStyle = ref({ left: '0px', top: '0px' })
const durationDraft = ref(String(props.modelValue.duration))
const ratios: { value: VideoOptions['ratio']; label: string; width: number; height: number }[] = [
  { value: 'adaptive', label: 'Auto', width: 15, height: 11 },
  { value: '16:9', label: '16:9', width: 19, height: 11 },
  { value: '4:3', label: '4:3', width: 16, height: 12 },
  { value: '1:1', label: '1:1', width: 13, height: 13 },
  { value: '3:4', label: '3:4', width: 12, height: 16 },
  { value: '9:16', label: '9:16', width: 10, height: 18 },
  { value: '21:9', label: '21:9', width: 21, height: 9 },
]
const provider = computed<NodeProvider>(() => (props.selection ? (props.selection.modelId ? 'apimart' : 'none') : 'ark'))
const pickerModelId = computed(() => props.selection ? modelOptionIdFor(props.selection, 'video') : (props.videoModel ?? VIDEO_MODEL))
const apimartModel = computed(() => (provider.value === 'apimart' ? catalogModelFor(props.selection!.modelId, 'video', props.selection!.parameters, selectedMode.value) : undefined))
const parameterValues = computed(() => apimartModel.value ? parameterValuesFor(apimartModel.value, props.selection?.parameters ?? {}) : {})
const model = computed(() => videoModelDefinition(props.videoModel ?? VIDEO_MODEL))
const supportedModes = computed<readonly VideoMode[]>(() => provider.value === 'ark' ? ['text', 'reference'] : apimartVideoModes(props.selection?.modelId))
const modeAvailable = computed(() => supportedModes.value.length > 0)
const selectedMode = computed(() => compatibleVideoMode(props.videoMode ?? 'reference', supportedModes.value) ?? 'text')
const modeName = computed(() => VIDEO_MODES.find((mode) => mode.value === selectedMode.value)!.name)
const referenceDescription = computed(() => provider.value === 'apimart' ? '使用参考图片生成视频' : '图片、视频、音频参考')
const referenceReason = computed(() => {
  if (selectedMode.value === 'text') return ''
  if (selectedMode.value === 'image') return props.referenceError || '请选择一张起始图片'
  if (!props.referenceCount) return provider.value === 'apimart' ? '全能参考需至少添加 1 张参考图片' : '全能参考需至少添加 1 项参考素材'
  return props.referenceError
})
const summary = computed(() => {
  if (provider.value === 'apimart' && apimartModel.value) {
    const text = parameterSummary(apimartModel.value, parameterValues.value)
    return text || '设置参数'
  }
  if (provider.value !== 'ark') return '设置参数'
  return `${props.modelValue.ratio === 'adaptive' ? 'Auto' : props.modelValue.ratio} · ${props.modelValue.resolution.toUpperCase()} · ${props.modelValue.duration}s`
})
const durationProgress = computed(() => `${(props.modelValue.duration - model.value.minDuration) / (model.value.maxDuration - model.value.minDuration) * 100}%`)

function trigger(menu: Menu) { return root.value?.querySelector<HTMLButtonElement>(`[data-menu="${menu}"]`) }
function positionMenu() {
  if (!open.value || !popup.value) return
  const anchor = trigger(open.value)?.getBoundingClientRect()
  if (!anchor) return
  const box = popup.value.getBoundingClientRect()
  const above = anchor.top - box.height - 8
  menuStyle.value = {
    left: `${Math.max(16, Math.min(anchor.left, window.innerWidth - box.width - 16))}px`,
    top: `${Math.max(16, Math.min(above >= 16 ? above : anchor.bottom + 8, window.innerHeight - box.height - 16))}px`,
  }
}
async function show(menu: Menu) {
  if (open.value === menu) { close(); return }
  if (menu === 'model') {
    modelSelect.value?.close()
    open.value = null
    return
  }
  open.value = menu
  await nextTick()
  positionMenu()
  const selected = popup.value?.querySelector<HTMLElement>('input:checked, input[type="text"], input[type="number"]')
  ;(selected ?? popup.value)?.focus({ preventScroll: true })
}
function close(restoreFocus = false) {
  const previous = open.value
  open.value = null
  if (restoreFocus && previous) trigger(previous)?.focus({ preventScroll: true })
}
function update<K extends keyof VideoOptions>(key: K, value: VideoOptions[K]) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
function editDuration(event: Event) {
  const value = (event.target as HTMLInputElement).value
  durationDraft.value = value
  const duration = Number(value)
  if (value && Number.isInteger(duration) && duration >= model.value.minDuration && duration <= model.value.maxDuration) update('duration', duration)
}
function finishDuration() {
  const value = Number(durationDraft.value)
  const duration = durationDraft.value && Number.isFinite(value) ? Math.min(model.value.maxDuration, Math.max(model.value.minDuration, Math.round(value))) : props.modelValue.duration
  durationDraft.value = String(duration)
  update('duration', duration)
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (!open.value) {
    if (['ArrowDown', 'ArrowUp'].includes(event.key) && event.target instanceof HTMLElement && event.target.dataset.menu) {
      event.preventDefault(); event.stopPropagation()
      void show(event.target.dataset.menu as Menu)
    }
    return
  }
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true) }
}
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) close()
}
function focusout(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && !root.value?.contains(event.relatedTarget)) close()
}
watch(() => props.modelValue.duration, (value) => { durationDraft.value = String(value) })
watch(() => props.panelPosition, positionMenu, { flush: 'post' })
// 多源 watch 按元素比较；单选 getter 返回新数组会在每次 props 更新时误触发。
watch([() => props.videoModel, () => pickerModelId.value], () => close())
onMounted(() => document.addEventListener('pointerdown', outside, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside, true))
defineExpose({ close })
</script>

<template>
  <div ref="root" class="video-generation-controls" @keydown="keydown" @focusout="focusout">
    <div class="video-model-wrap">
      <ImageModelSelect ref="modelSelect" :model-value="pickerModelId" :provider="provider" :category="'video'" :panel-position="panelPosition" @choose="emit('choose-model', $event)" />
    </div>
    <button v-if="modeAvailable" data-menu="mode" type="button" class="video-control mode-control" :class="{ 'is-open': open === 'mode' }" :aria-label="`选择视频生成类型，当前${modeName}`" :title="modeName" aria-haspopup="listbox" :aria-expanded="open === 'mode'" :aria-controls="open === 'mode' ? menuId : undefined" @click="show('mode')">
      <svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><rect x="3" y="5" width="12" height="12" rx="2" /><path d="M12 3h5m-2-2v5M6 13l3-3 3 3" /></svg>
      <span>{{ modeName }}</span>
      <svg class="chevron" viewBox="0 0 16 16" aria-hidden="true"><path :d="open === 'mode' ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg>
    </button>
    <span v-if="modeAvailable" class="control-divider" aria-hidden="true"></span>
    <button data-menu="parameters" type="button" class="video-control parameter-control" :class="{ 'is-open': open === 'parameters' }" :aria-label="`视频参数，${summary}`" aria-haspopup="dialog" :aria-expanded="open === 'parameters'" :aria-controls="open === 'parameters' ? menuId : undefined" @click="show('parameters')">
      <svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><rect x="2" y="5" width="16" height="10" rx="1.5" /></svg>
      <span>{{ summary }}</span>
      <svg class="chevron" viewBox="0 0 16 16" aria-hidden="true"><path :d="open === 'parameters' ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg>
    </button>

    <div v-if="open" :id="menuId" ref="popup" class="video-popover" :class="`popover-${open}`" :style="menuStyle" :role="open === 'parameters' ? 'dialog' : 'listbox'" :aria-label="open === 'mode' ? '视频生成模式' : '视频参数'" tabindex="-1" @wheel.stop>
      <template v-if="open === 'mode'">
        <p class="popover-heading">视频生成模式</p>
        <button v-for="mode in VIDEO_MODES" :key="mode.value" type="button" role="option" :aria-selected="selectedMode === mode.value" :disabled="!supportedModes.includes(mode.value)" class="video-menu-option" :class="{ 'is-selected': selectedMode === mode.value }" @click="emit('update:videoMode', mode.value); close(true)">
          <svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><rect x="3" y="5" width="12" height="12" rx="2" /><path d="M6 13l3-3 3 3" /></svg>
          <span class="option-copy"><strong>{{ mode.name }}</strong><small>{{ mode.value === 'reference' ? referenceDescription : mode.description }}</small></span>
          <svg v-if="selectedMode === mode.value" class="checkmark" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 8 3 3 7-7" /></svg>
        </button>
        <p v-if="referenceReason" :id="`${menuId}-reason`" class="mode-reason">{{ referenceReason }}</p>
      </template>
      <template v-else>
        <template v-if="provider === 'apimart'">
          <CatalogParameterForm v-if="apimartModel" :model="apimartModel" :parameters="parameterValues" @update="(key, value) => emit('update:parameter', key, value)" @resize="positionMenu" />
          <p v-else class="parameter-placeholder" role="status">该历史模型已移出当前列表，原选择与参数继续保留。</p>
        </template>
        <template v-else-if="provider === 'none'">
          <p class="parameter-placeholder" role="status">请先选择模型，再设置对应参数。</p>
        </template>
        <template v-else>
          <fieldset class="parameter-section" aria-label="视频比例">
            <legend>比例</legend>
            <div class="ratio-grid">
              <label v-for="ratio in ratios.filter((choice) => model.ratios.includes(choice.value))" :key="ratio.value" class="choice ratio-choice" :class="{ 'is-selected': modelValue.ratio === ratio.value }">
                <input type="radio" :name="`${menuId}-ratio`" :value="ratio.value" :aria-label="ratio.value === 'adaptive' ? '自动比例' : ratio.label" :checked="modelValue.ratio === ratio.value" @change="update('ratio', ratio.value)" />
                <span class="ratio-symbol" :class="{ 'is-adaptive': ratio.value === 'adaptive' }" :style="{ width: `${ratio.width}px`, height: `${ratio.height}px` }" aria-hidden="true"></span>
                <span>{{ ratio.label }}</span>
              </label>
            </div>
          </fieldset>
          <fieldset class="parameter-section" aria-label="视频画质">
            <legend>清晰度</legend>
            <div class="resolution-grid">
              <label v-for="resolution in model.resolutions" :key="resolution" class="choice resolution-choice" :class="{ 'is-selected': modelValue.resolution === resolution }">
                <input type="radio" :name="`${menuId}-resolution`" :value="resolution" :aria-label="resolution.toUpperCase()" :checked="modelValue.resolution === resolution" @change="update('resolution', resolution)" />
                {{ resolution.toUpperCase() }}
              </label>
            </div>
          </fieldset>
          <div class="parameter-section">
            <label :for="`${menuId}-duration`" class="parameter-label">视频时长</label>
            <div class="duration-controls">
              <input :id="`${menuId}-duration`" class="duration-slider" type="range" :min="model.minDuration" :max="model.maxDuration" step="1" :value="modelValue.duration" :aria-valuetext="`${modelValue.duration} 秒`" :style="{ '--duration-progress': durationProgress }" @input="editDuration" />
              <input class="duration-number" type="number" aria-label="视频时长秒数" :min="model.minDuration" :max="model.maxDuration" step="1" :value="durationDraft" @input="editDuration" @change="finishDuration" @blur="finishDuration" @keydown.enter.prevent="finishDuration" />
              <span class="duration-unit">s</span>
            </div>
            <div class="duration-limits" aria-hidden="true"><span>{{ model.minDuration }} 秒</span><span>{{ model.maxDuration }} 秒</span></div>
          </div>
          <label class="audio-option"><input type="checkbox" :checked="modelValue.generateAudio" @change="update('generateAudio', ($event.target as HTMLInputElement).checked)" />生成声音</label>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.video-generation-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 2px; min-width: 0; }
.video-model-wrap { min-width: 0; }
.video-control { display: inline-flex; align-items: center; gap: 5px; min-height: 32px; padding: 0 5px; border: 0; border-radius: 7px; color: #e2e2e2; background: transparent; font: inherit; font-size: 12px; cursor: pointer; white-space: nowrap; }
.video-control:hover, .video-control.is-open { background: #3b3b3b; color: #fff; }
.video-control.is-unavailable { color: #929292; }
.control-divider { height: 14px; width: 1px; margin: 0 4px; background: #4a4a4a; }
svg { fill: none; stroke: currentColor; stroke-width: 1.4; stroke-linecap: round; stroke-linejoin: round; flex: none; }
.model-icon { width: 18px; height: 18px; }
.control-icon { width: 16px; height: 16px; }
.chevron { width: 10px; height: 10px; color: #969696; }
.video-popover { position: fixed; z-index: 3; width: min(342px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; overscroll-behavior: contain; border: 1px solid #4b4b4b; border-radius: 15px; padding: 12px; background: #262626; color: #e4e4e4; box-shadow: 0 10px 32px #0005; scrollbar-width: thin; scrollbar-color: #555 transparent; }
.popover-mode { width: min(248px, calc(100vw - 32px)); padding: 7px; }
.popover-heading { margin: 8px 9px 10px; color: #a8a8a8; font-size: 12px; }
.video-menu-option { display: flex; align-items: center; gap: 9px; width: 100%; min-height: 48px; padding: 9px; border: 0; border-radius: 9px; color: #e4e4e4; background: transparent; text-align: left; font: inherit; cursor: pointer; }
.video-menu-option.is-selected { background: #414141; }
.video-menu-option:hover:not(:disabled) { background: #494949; }
.video-menu-option:disabled { opacity: 1; color: #858585; cursor: not-allowed; }
.option-icon { display: grid; place-items: center; width: 32px; height: 34px; border-radius: 8px; background: #ffffff09; }
.option-copy { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.option-copy strong { font-size: 13px; font-weight: 500; }
.option-copy small { color: #b1b1b1; font-size: 11px; }
.checkmark { width: 15px; height: 15px; margin-left: auto; }
.mode-reason { margin: 7px 9px; padding-top: 10px; border-top: 1px solid #414141; color: #b7aaa0; font-size: 11px; line-height: 1.7; }
.parameter-section { padding: 0; margin: 0 0 14px; border: 0; min-width: 0; }
.parameter-section legend, .parameter-label { padding: 0; margin-bottom: 9px; color: #a8a8a8; font-size: 12px; }
.parameter-label { display: block; }
.ratio-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 8px; }
.choice { position: relative; display: flex; align-items: center; justify-content: center; border: 1px solid #515151; border-radius: 8px; color: #b5b5b5; font-size: 13px; cursor: pointer; }
.choice:hover { background: #323232; }
.choice.is-selected { border-color: #e4e4e4; background: #3b3b3b; color: #fff; }
.choice input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.choice:has(input:focus-visible) { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.ratio-choice { flex-direction: column; gap: 9px; height: 64px; }
.ratio-symbol { display: block; border: 1px solid currentColor; border-radius: 1px; }
.ratio-symbol.is-adaptive { border-style: dashed; }
.resolution-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.resolution-choice { min-height: 34px; }
.duration-controls { display: flex; gap: 9px; align-items: center; }
.duration-slider { flex: 1; min-width: 0; appearance: none; height: 4px; margin: 12px 0; border-radius: 3px; cursor: pointer; background: linear-gradient(to right, #4797e5 var(--duration-progress), #484848 var(--duration-progress)); }
.duration-slider::-webkit-slider-thumb { appearance: none; width: 11px; height: 11px; border-radius: 50%; background: #fff; }
.duration-slider::-moz-range-thumb { width: 11px; height: 11px; border: 0; border-radius: 50%; background: #fff; }
.duration-number { width: 48px; min-height: 27px; padding: 3px 5px; border: 1px solid transparent; border-radius: 6px; background: #363636; color: #f1f1f1; text-align: center; font: inherit; font-size: 13px; appearance: textfield; }
.duration-number::-webkit-inner-spin-button, .duration-number::-webkit-outer-spin-button { appearance: none; margin: 0; }
.duration-number:focus-visible, .duration-slider:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.duration-unit { font-size: 12px; color: #a8a8a8; }
.duration-limits { display: flex; justify-content: space-between; margin-right: 72px; color: #909090; font-size: 10px; }
.audio-option { display: flex; align-items: center; gap: 7px; padding-top: 11px; border-top: 1px solid #414141; font-size: 12px; color: #c4c4c4; cursor: pointer; }
.audio-option input { margin: 0; accent-color: #c9c9c9; }
.parameter-placeholder { margin: 2px 0 0; color: #a8a8a8; font-size: 12px; line-height: 1.6; }
@media (max-width: 600px) { .control-divider { display: none; } }
</style>
