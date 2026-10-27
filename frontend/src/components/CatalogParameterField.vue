<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { deepEqual, parameterEnum, parameterRange, validParameterValue, type CatalogParameter } from '../models/modelCatalog'

const props = defineProps<{ parameter: CatalogParameter; parameters: Record<string, unknown> }>()
const emit = defineEmits<{ update: [key: string, value: unknown] }>()
const fieldId = useId()
const fixed = computed(() => props.parameter.kind === 'fixed')
const value = computed(() => fixed.value ? props.parameter.fixed : props.parameters[props.parameter.key])
const boolean = computed(() => props.parameter.kind === 'boolean' || (fixed.value && typeof props.parameter.fixed === 'boolean'))
const hasValue = computed(() => value.value !== undefined && value.value !== null && value.value !== '')
const invalid = computed(() => hasValue.value && !validParameterValue(props.parameter, value.value, props.parameters))
const labels: Record<string, string> = {
  ratio: '比例', resolution: '清晰度', duration: '视频时长', count: '生成数量', audio: '生成音频',
}
const label = computed(() => labels[props.parameter.role] ?? labels[props.parameter.key] ?? props.parameter.label)
const numeric = computed(() => props.parameter.kind === 'integer' || props.parameter.kind === 'number')
const step = computed(() => {
  const declared = props.parameter.step
  if (declared != null && Number.isFinite(declared) && declared > 0) return declared
  return props.parameter.kind === 'integer' ? 1 : undefined
})
// multipleOf 从 0 开始计数；滑块端点也必须落在上游允许的步长上。
const bounds = computed(() => {
  const range = parameterRange(props.parameter, props.parameters)
  const minimum = range.minimum != null && step.value ? Math.ceil(range.minimum / step.value - 1e-9) * step.value : range.minimum
  const maximum = range.maximum != null && step.value ? Math.floor(range.maximum / step.value + 1e-9) * step.value : range.maximum
  return { minimum, maximum }
})
const bounded = computed(() => bounds.value.minimum != null && bounds.value.maximum != null && bounds.value.maximum > bounds.value.minimum)
const options = computed(() => {
  const declared = parameterEnum(props.parameter, props.parameters)
  if (declared) return declared
  // 小范围整数张数直接选择；不补齐离散枚举中没有的张数。
  if (props.parameter.role === 'count' && props.parameter.kind === 'integer' && bounded.value) {
    const { minimum, maximum } = bounds.value
    const increment = step.value ?? 1
    const count = Math.floor((maximum! - minimum!) / increment) + 1
    if (count <= 8) return Array.from({ length: count }, (_, index) => minimum! + index * increment)
  }
  return []
})
const isRatio = computed(() => props.parameter.role === 'ratio')
const isAudio = computed(() => props.parameter.role === 'audio')
const custom = ref(false)
const manualNumber = ref<number>()
const specialValue = computed(() => props.parameter.specialValues?.find((option) => deepEqual(option.value, value.value)))
watch(value, (next) => {
  if (typeof next === 'number' && !specialValue.value && validParameterValue(props.parameter, next, props.parameters)) manualNumber.value = next
  if (props.parameter.allowCustom && hasValue.value) custom.value = !options.value.some((option) => deepEqual(option, next))
}, { immediate: true })
const unit = computed(() => props.parameter.role === 'duration' ? 's' : props.parameter.role === 'count' ? props.parameter.unit ?? '张' : '')
const progress = computed(() => {
  if (!bounded.value || !hasValue.value) return '0%'
  const fraction = (Number(value.value) - bounds.value.minimum!) / (bounds.value.maximum! - bounds.value.minimum!)
  return `${Math.min(100, Math.max(0, fraction * 100))}%`
})
const automatic = (option: unknown) => ['auto', 'adaptive'].includes(String(option).toLowerCase())
function optionLabel(option: unknown, display = false): string {
  const configured = props.parameter.optionLabels?.[String(option)]
  if (configured) return configured
  if (isRatio.value && automatic(option)) return display ? 'Auto' : '自动比例'
  if (props.parameter.role === 'count') return `${String(option)}${unit.value}`
  if (props.parameter.role === 'duration') return `${String(option)}s`
  return display && props.parameter.role === 'resolution' ? String(option).toUpperCase() : String(option)
}
function ratioStyle(option: unknown) {
  const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(String(option))
  if (!match) return { width: '15px', height: '11px' }
  const width = Number(match[1]), height = Number(match[2])
  const scale = 19 / Math.max(width, height)
  return { width: `${Math.max(4, width * scale)}px`, height: `${Math.max(4, height * scale)}px` }
}
function update(next: unknown) { emit('update', props.parameter.key, next) }
function choose(option: unknown) { custom.value = false; update(option) }
function chooseManualNumber() {
  const next = [manualNumber.value, props.parameter.default, bounds.value.minimum].find((candidate) =>
    typeof candidate === 'number' && !props.parameter.specialValues?.some((option) => deepEqual(option.value, candidate)) && validParameterValue(props.parameter, candidate, props.parameters))
  update(next)
}
function editNumber(event: Event) {
  const input = event.target as HTMLInputElement
  if (!input.value.trim()) { update(undefined); return }
  let next = Number(input.value)
  if (!Number.isFinite(next)) return
  if (step.value) next = Number((Math.round(next / step.value) * step.value).toPrecision(12))
  if (bounds.value.minimum != null) next = Math.max(bounds.value.minimum, next)
  if (bounds.value.maximum != null) next = Math.min(bounds.value.maximum, next)
  if (!validParameterValue(props.parameter, next, props.parameters)) return
  input.value = String(next)
  update(next)
}
</script>

<template>
  <fieldset class="parameter-section" :class="{ 'is-boolean': boolean, 'is-audio': boolean && isAudio }" :aria-label="parameter.label">
    <legend :class="{ 'visually-hidden': boolean }">
      {{ label }}
      <span v-if="parameter.required && !hasValue" class="parameter-required">必选</span>
      <span v-if="invalid" class="parameter-warning">值已失效</span>
      <span v-else-if="parameter.unresolved" class="parameter-warning">约束待确认</span>
    </legend>
    <p v-if="fixed && !boolean" class="fixed-value" aria-readonly="true">{{ optionLabel(value, true) }}</p>
    <div v-else-if="options.length" class="option-grid" :class="{ 'ratio-grid': isRatio }">
      <label v-for="option in options" :key="JSON.stringify(option)" class="choice" :class="{ 'ratio-choice': isRatio, 'is-selected': !custom && hasValue && deepEqual(value, option) }">
        <input type="radio" :name="fieldId" :value="String(option)" :aria-label="optionLabel(option)" :checked="!custom && hasValue && deepEqual(value, option)" @change="choose(option)" />
        <span v-if="isRatio" class="ratio-icon" aria-hidden="true"><span class="ratio-symbol" :class="{ 'is-adaptive': automatic(option) }" :style="ratioStyle(option)"></span></span>
        <span>{{ optionLabel(option, true) }}</span>
      </label>
      <label v-if="parameter.allowCustom" class="choice" :class="{ 'ratio-choice': isRatio, 'is-selected': custom }">
        <input type="radio" :name="fieldId" :aria-label="`自定义${label}`" :checked="custom" @change="custom = true" />
        <span v-if="isRatio" class="ratio-icon custom-icon" aria-hidden="true">···</span>
        <span>自定义</span>
      </label>
    </div>
    <template v-else-if="numeric">
      <div v-if="parameter.specialValues?.length" class="option-grid number-modes">
        <label class="choice" :class="{ 'is-selected': !specialValue }">
          <input type="radio" :name="`${fieldId}-mode`" :aria-label="`自定义${label}`" :checked="!specialValue" @change="chooseManualNumber" />
          <span>自定义</span>
        </label>
        <label v-for="option in parameter.specialValues" :key="option.value" class="choice" :class="{ 'is-selected': specialValue?.value === option.value }">
          <input type="radio" :name="`${fieldId}-mode`" :aria-label="option.label" :checked="specialValue?.value === option.value" @change="update(option.value)" />
          <span>{{ option.label }}</span>
        </label>
      </div>
      <template v-if="!specialValue">
      <div class="number-controls" :class="{ 'is-bounded': bounded, 'is-single': !bounded }">
        <input v-if="bounded" class="parameter-slider" :class="{ 'is-unset': !hasValue }" type="range" :min="bounds.minimum!" :max="bounds.maximum!" :step="step ?? 'any'" :value="hasValue ? Number(value) : bounds.minimum!" :aria-label="`${parameter.label}滑杆`" :aria-valuetext="hasValue ? `${value}${unit}` : '尚未设置'" :style="{ '--parameter-progress': progress }" @input="editNumber" />
        <input :id="fieldId" class="parameter-number" type="number" :min="bounds.minimum ?? undefined" :max="bounds.maximum ?? undefined" :step="step ?? 'any'" :value="hasValue ? String(value) : ''" :aria-label="parameter.label" :placeholder="`填写${label}`" @change="editNumber" />
        <span v-if="unit" class="parameter-unit">{{ unit }}</span>
      </div>
      <div v-if="bounded" class="range-limits" aria-hidden="true"><span>{{ bounds.minimum }}{{ unit === 's' ? ' 秒' : unit }}</span><span>{{ bounds.maximum }}{{ unit === 's' ? ' 秒' : unit }}</span></div>
      <p v-else class="range-note">{{ bounds.minimum != null ? `至少 ${bounds.minimum}${unit === 's' ? ' 秒' : unit}` : bounds.maximum != null ? `最多 ${bounds.maximum}${unit === 's' ? ' 秒' : unit}` : '可选范围未提供' }}</p>
      </template>
      <p v-else class="range-note">按所选模式自动确定时长。</p>
    </template>
    <label v-else-if="boolean" class="boolean-option" :class="{ 'audio-option': isAudio, 'switch-option': !isAudio, 'is-fixed': fixed }">
      <input v-if="isAudio" type="checkbox" :checked="value === true" :disabled="fixed" @change="update(($event.target as HTMLInputElement).checked)" />
      <span class="boolean-label">{{ label }}<span v-if="parameter.required && !hasValue" class="parameter-required">待设置</span></span>
      <input v-if="!isAudio" class="parameter-switch" type="checkbox" role="switch" :checked="value === true" :disabled="fixed" @change="update(($event.target as HTMLInputElement).checked)" />
    </label>
    <template v-else-if="parameter.kind === 'string'">
      <input :id="fieldId" class="parameter-text" type="text" :value="hasValue ? String(value) : ''" :aria-label="parameter.label" :placeholder="`填写${label}`" autocomplete="off" @change="update(($event.target as HTMLInputElement).value || undefined)" />
      <p v-if="['ratio', 'resolution', 'size'].includes(parameter.role)" class="range-note">可选范围未提供</p>
    </template>
    <p v-else class="range-note">可选项待补充</p>
    <input v-if="options.length && parameter.allowCustom && custom" :id="fieldId" class="parameter-text custom-input" type="text" :value="hasValue ? String(value) : ''" :aria-label="`${parameter.label}自定义值`" :placeholder="`填写${label}`" autocomplete="off" @change="update(($event.target as HTMLInputElement).value || undefined)" />
    <p v-if="parameter.note" class="range-note parameter-note">{{ parameter.note }}</p>
  </fieldset>
</template>

<style scoped>
.parameter-section { min-width: 0; margin: 0 0 14px; padding: 0; border: 0; }
.parameter-section:last-child { margin-bottom: 0; }
.parameter-section legend { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; margin-bottom: 9px; padding: 0; color: #a8a8a8; font-size: 12px; }
.parameter-required, .parameter-warning { color: #d3b48a; font-size: 10px; }
.option-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(68px, 1fr)); gap: 8px; }
.ratio-grid { grid-template-columns: repeat(5, minmax(0, 1fr)); }
.fixed-value { display: flex; align-items: center; justify-content: center; min-height: 34px; box-sizing: border-box; margin: 0; padding: 4px 8px; border: 1px solid #737373; border-radius: 8px; background: #3b3b3b; color: #e4e4e4; font-size: 13px; }
.boolean-option.is-fixed { cursor: default; }
.choice { position: relative; display: flex; align-items: center; justify-content: center; min-width: 0; min-height: 34px; padding: 4px 8px; border: 1px solid #515151; border-radius: 8px; color: #b5b5b5; font-size: 13px; overflow-wrap: anywhere; text-align: center; cursor: pointer; box-sizing: border-box; }
.choice:hover { background: #323232; }
.choice.is-selected { border-color: #e4e4e4; background: #3b3b3b; color: #fff; }
.choice input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.choice:has(input:focus-visible) { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.ratio-choice { flex-direction: column; gap: 9px; height: 64px; }
.ratio-icon { display: grid; place-items: center; height: 18px; }
.ratio-symbol { display: block; border: 1px solid currentColor; border-radius: 1px; }
.ratio-symbol.is-adaptive { border-style: dashed; }
.custom-icon { font-size: 18px; line-height: 1; }
.custom-input { margin-top: 8px; }
.number-modes { margin-bottom: 9px; }
.parameter-note { line-height: 1.6; }
.number-controls { display: flex; gap: 9px; align-items: center; }
.number-controls.is-single { position: relative; }
.parameter-slider { flex: 1; min-width: 0; appearance: none; height: 4px; margin: 12px 0; border-radius: 3px; cursor: pointer; background: linear-gradient(to right, #4797e5 var(--parameter-progress), #484848 var(--parameter-progress)); }
.parameter-slider::-webkit-slider-thumb { appearance: none; width: 11px; height: 11px; border-radius: 50%; background: #fff; }
.parameter-slider::-moz-range-thumb { width: 11px; height: 11px; border: 0; border-radius: 50%; background: #fff; }
.parameter-slider.is-unset { opacity: .5; }
.parameter-number { width: 60px; min-height: 27px; padding: 3px 5px; border: 1px solid transparent; border-radius: 6px; background: #363636; color: #f1f1f1; text-align: center; font: inherit; font-size: 13px; appearance: textfield; }
.is-bounded .parameter-number { width: 48px; }
.parameter-number::-webkit-inner-spin-button, .parameter-number::-webkit-outer-spin-button { appearance: none; margin: 0; }
.parameter-number:focus-visible, .parameter-slider:focus-visible, .parameter-text:focus-visible, .parameter-switch:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.is-single .parameter-number { box-sizing: border-box; width: 100%; min-width: 0; min-height: 34px; padding: 6px 30px; border-color: #515151; border-radius: 8px; background: transparent; }
.is-single .parameter-number:hover, .parameter-text:hover { border-color: #737373; }
.is-single .parameter-number:focus, .parameter-text:focus { border-color: #e4e4e4; background: #3b3b3b; }
.parameter-number::placeholder, .parameter-text::placeholder { color: #858585; }
.parameter-unit { flex: none; font-size: 12px; color: #a8a8a8; }
.is-single .parameter-unit { position: absolute; right: 11px; pointer-events: none; }
.range-limits { display: flex; justify-content: space-between; margin-top: 3px; margin-right: 72px; color: #909090; font-size: 10px; }
.range-note { margin: 5px 0 0; color: #909090; font-size: 10px; }
.is-audio { padding-top: 11px; border-top: 1px solid #414141; }
.boolean-option { display: flex; align-items: center; gap: 7px; min-height: 20px; font-size: 12px; color: #c4c4c4; cursor: pointer; }
.boolean-label { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; min-width: 0; }
.audio-option input { margin: 0; accent-color: #c9c9c9; }
.audio-option input:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 3px; }
.switch-option { justify-content: space-between; color: #a8a8a8; }
.parameter-switch { position: relative; flex: none; appearance: none; box-sizing: border-box; width: 38px; height: 20px; margin: 0; border: 1px solid transparent; border-radius: 12px; background: #585858; cursor: pointer; }
.parameter-switch::before { content: ''; position: absolute; left: 3px; top: 3px; width: 12px; height: 12px; border-radius: 50%; background: #202020; }
.parameter-switch:checked { background: #c9c9c9; }
.parameter-switch:checked::before { transform: translateX(18px); }
.parameter-switch:hover { border-color: #8a8a8a; }
.parameter-text { width: 100%; min-width: 0; box-sizing: border-box; min-height: 34px; padding: 6px 10px; border: 1px solid #515151; border-radius: 8px; background: transparent; color: #eee; font: inherit; font-size: 13px; text-align: center; }
.visually-hidden { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; clip-path: inset(50%); overflow: hidden; white-space: nowrap; }
</style>
