<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { draftParts, normalizeParts, partLength, promptText, sliceParts, type MentionCandidate, type PromptPart } from '../canvas/prompt'
import { editorSelection, readPromptDOM, restoreEditorSelection, type EditorSelection } from '../canvas/promptDom'

const props = defineProps<{
  nodeId: string; modelValue: string; parts?: PromptPart[]; label: string; placeholder: string
  candidates: MentionCandidate[]; addReference: (id: string) => string
}>()
const emit = defineEmits<{ change: [parts: PromptPart[]] }>()
const editor = ref<HTMLElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const current = ref<PromptPart[]>([])
const open = ref(false)
const query = ref('')
const category = ref<'image' | 'video' | null>(null)
const compact = ref(false)
const activeIndex = ref(0)
const menuStyle = ref<Record<string, string>>({})
const menuError = ref('')
let trigger = { start: 0, end: 0 }
let composing = false
let bookmark: EditorSelection = { anchor: 0, focus: 0 }
type EditState = { parts: PromptPart[]; selection: EditorSelection }
let before: EditState | undefined
let undo: EditState[] = []
let redo: EditState[] = []
let transactionTime = 0
let lastInputType = ''
const clone = (parts: PromptPart[]) => parts.map((part) => ({ ...part }))
const matching = computed(() => props.candidates.filter((item) => `${item.name} ${item.label}`.toLocaleLowerCase().includes(query.value.toLocaleLowerCase().trim())))
const selected = computed(() => matching.value.filter((item) => item.selected))
const resources = computed(() => matching.value.filter((item) => item.kind === category.value))
const choices = computed(() => category.value
  ? resources.value.map((item) => ({ key: `asset:${item.id}`, item, kind: null }))
  : [...selected.value.map((item) => ({ key: `selected:${item.id}`, item, kind: null })),
    ...(['image', 'video'] as const).map((kind) => ({ key: kind, item: null, kind }))])
const activeKey = computed(() => choices.value[activeIndex.value]?.key)
watch([query, category], () => { activeIndex.value = 0; menuError.value = ''; void nextTick(positionMenu) })

function capture() {
  bookmark = editor.value ? editorSelection(editor.value) ?? bookmark : bookmark
  return { ...bookmark }
}
function focus() { editor.value?.focus({ preventScroll: true }) }
function restore(value: EditorSelection) {
  focus()
  if (editor.value) restoreEditorSelection(editor.value, value)
  bookmark = value
}
function closeMenu() { open.value = false; category.value = null; menuError.value = '' }
function makeChip(part: Extract<PromptPart, { type: 'reference' }>) {
  const element = document.createElement('span')
  element.contentEditable = 'false'
  element.dataset.promptReference = 'true'
  element.dataset.nodeId = part.nodeId
  element.dataset.kind = part.kind
  element.dataset.name = part.name
  paintChip(element)
  return element
}
function paintChip(element: HTMLElement) {
  const source = props.candidates.find((item) => item.id === element.dataset.nodeId)
  const error = !source ? '来源素材已不存在' : !source.selected ? '已移除参考，请重新引用' : source.error
  element.className = `prompt-chip${error ? ' is-invalid' : ''}`
  element.title = `${source?.name ?? element.dataset.name}${error ? `：${error}` : ''}`
  element.setAttribute('aria-label', `${source?.label || element.dataset.name}${error ? `，${error}` : ''}`)
  element.replaceChildren()
  if (source?.kind === 'image' && source.url) {
    const img = document.createElement('img')
    img.src = source.url; img.alt = ''; img.draggable = false
    element.append(img)
  } else {
    const icon = document.createElement('span')
    icon.className = 'chip-icon'; icon.dataset.kind = element.dataset.kind; icon.setAttribute('aria-hidden', 'true')
    element.append(icon)
  }
  const label = document.createElement('span')
  label.textContent = error ? `${element.dataset.name} · 失效` : source!.label
  element.append(label)
}
function render() {
  if (!editor.value) return
  editor.value.replaceChildren(...current.value.map((part) => part.type === 'text' ? document.createTextNode(part.text) : makeChip(part)))
  // A trailing newline needs a final line box; otherwise Chromium inserts the
  // next typed character before that newline despite a logical end selection.
  const last = current.value.at(-1)
  if (last?.type === 'text' && last.text.endsWith('\n')) {
    const tail = document.createElement('br')
    tail.dataset.promptTail = 'true'
    editor.value.append(tail)
  }
}
function publish(parts: PromptPart[]) {
  current.value = normalizeParts(parts)
  emit('change', clone(current.value))
}
function state(): EditState { return { parts: clone(current.value), selection: capture() } }
function remember(previous: EditState, type = '') {
  const now = Date.now()
  if (type !== 'insertText' || lastInputType !== type || now - transactionTime > 700 || previous.selection.anchor !== previous.selection.focus) {
    undo.push(previous)
    if (undo.length > 100) undo.shift()
  }
  lastInputType = type; transactionTime = now; redo = []
}
function replace(start: number, end: number, inserted: PromptPart[]) {
  remember(state())
  publish([...sliceParts(current.value, 0, start), ...inserted, ...sliceParts(current.value, end)])
  render()
  const caret = start + inserted.reduce((sum, part) => sum + partLength(part), 0)
  restore({ anchor: caret, focus: caret })
}
function insertText(text: string) {
  const selection = capture()
  replace(Math.min(selection.anchor, selection.focus), Math.max(selection.anchor, selection.focus), [{ type: 'text', text }])
  refreshTrigger()
}
function history(forward = false) {
  const source = forward ? redo : undo
  const next = source.pop()
  if (!next) return
  ;(forward ? undo : redo).push(state())
  publish(next.parts); render(); restore(next.selection)
  closeMenu(); lastInputType = ''
}
function beforeInput(event: InputEvent) {
  if (composing || event.isComposing) return
  if (event.inputType === 'historyUndo' || event.inputType === 'historyRedo') {
    event.preventDefault(); history(event.inputType === 'historyRedo'); return
  }
  if (['insertParagraph', 'insertLineBreak'].includes(event.inputType)) { event.preventDefault(); insertText('\n'); return }
  before = state()
}
function onInput(event: Event) {
  if (composing || (event as InputEvent).isComposing || !editor.value) return
  const parts = readPromptDOM(editor.value)
  if (JSON.stringify(parts) !== JSON.stringify(current.value)) {
    remember(before ?? { parts: clone(current.value), selection: bookmark }, (event as InputEvent).inputType)
    publish(parts)
  }
  if (editor.value.querySelector('br, div, p')) {
    const selection = capture()
    render()
    restore(selection)
  }
  before = undefined
  capture(); refreshTrigger()
}
function compositionStart() { before = state(); composing = true; closeMenu() }
function compositionEnd(event: CompositionEvent) { composing = false; onInput(event) }
function copy(event: ClipboardEvent, cut = false) {
  const selection = capture()
  const start = Math.min(selection.anchor, selection.focus), end = Math.max(selection.anchor, selection.focus)
  if (start === end) return
  event.preventDefault()
  event.clipboardData?.setData('text/plain', promptText(sliceParts(current.value, start, end)))
  if (cut) { replace(start, end, []); closeMenu() }
}
function paste(event: ClipboardEvent) {
  event.preventDefault()
  insertText((event.clipboardData?.getData('text/plain') ?? '').replace(/\r\n?/g, '\n'))
}
function refreshTrigger() {
  if (composing || !editor.value) return
  const selection = capture()
  if (selection.anchor !== selection.focus) { closeMenu(); return }
  const prefix = sliceParts(current.value, 0, selection.focus).map((part) => part.type === 'text' ? part.text : '\uFFFC').join('')
  const match = /@([^@\s\uFFFC]{0,80})$/.exec(prefix)
  if (!match) { closeMenu(); return }
  trigger = { start: selection.focus - match[0].length, end: selection.focus }
  query.value = match[1] ?? ''
  if (!open.value) { category.value = null; activeIndex.value = 0 }
  open.value = true
  void nextTick(positionMenu)
}
function positionMenu() {
  if (!open.value || !editor.value || !menu.value) return
  compact.value = window.innerWidth < 560
  const width = Math.min(category.value && !compact.value ? 452 : 232, window.innerWidth - 24)
  // The bookmarked @ range remains stable while typing in the menu search box.
  const focused = document.activeElement
  const savedSelection = editorSelection(editor.value)
  restoreEditorSelection(editor.value, { anchor: trigger.start, focus: trigger.end })
  const rect = window.getSelection()?.getRangeAt(0).getBoundingClientRect()
  if (savedSelection) restoreEditorSelection(editor.value, savedSelection)
  else window.getSelection()?.removeAllRanges()
  if (focused === search.value) search.value?.focus({ preventScroll: true })
  const fallback = editor.value.getBoundingClientRect()
  const anchor = rect?.height ? rect : fallback
  const maxHeight = Math.min(340, window.innerHeight - 24)
  const height = Math.min(menu.value.scrollHeight || 260, maxHeight)
  const top = anchor.bottom + 6 + height <= window.innerHeight - 12 ? anchor.bottom + 6 : Math.max(12, anchor.top - height - 6)
  menuStyle.value = { left: `${Math.max(12, Math.min(anchor.left, window.innerWidth - width - 12))}px`, top: `${top}px`, width: `${width}px`, maxHeight: `${maxHeight}px` }
}
async function choose(item: MentionCandidate) {
  if (item.error) { menuError.value = item.error; return }
  const error = props.addReference(item.id)
  if (error) { menuError.value = error; return }
  replace(trigger.start, trigger.end, [{ type: 'reference', nodeId: item.id, kind: item.kind, name: item.name }, { type: 'text', text: ' ' }])
  closeMenu()
  await nextTick()
  editor.value?.querySelectorAll<HTMLElement>('[data-prompt-reference]').forEach(paintChip)
}
function showCategory(kind: 'image' | 'video') { category.value = kind; activeIndex.value = 0 }
function menuKey(event: KeyboardEvent) {
  if (event.isComposing || composing) return false
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeMenu(); restore({ anchor: trigger.end, focus: trigger.end }); return true }
  if (['ArrowDown', 'ArrowUp', 'Enter', 'ArrowRight', 'ArrowLeft'].includes(event.key)) {
    // Left/right still edit a search query; vertical keys and Enter choose items.
    if (event.target === search.value && ['ArrowRight', 'ArrowLeft'].includes(event.key)) return false
    event.preventDefault(); event.stopPropagation()
    if (event.key === 'ArrowDown') activeIndex.value = (activeIndex.value + 1) % Math.max(1, choices.value.length)
    if (event.key === 'ArrowUp') activeIndex.value = (activeIndex.value - 1 + choices.value.length) % Math.max(1, choices.value.length)
    if (event.key === 'ArrowLeft') category.value = null
    const choice = choices.value[activeIndex.value]
    if (choice && (event.key === 'Enter' || event.key === 'ArrowRight')) {
      if (choice.kind) showCategory(choice.kind)
      else if (event.key === 'Enter' && choice.item) void choose(choice.item)
    }
    void nextTick(() => menu.value?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }))
    return true
  }
  if (event.key === 'Tab') closeMenu()
  return false
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing || composing) return
  if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) lastInputType = ''
  if ((event.ctrlKey || event.metaKey) && ['z', 'y'].includes(event.key.toLowerCase())) {
    event.preventDefault(); history(event.shiftKey || event.key.toLowerCase() === 'y'); return
  }
  if (open.value && menuKey(event)) return
  if (event.key === 'Enter') { event.preventDefault(); insertText('\n'); return }
  if (['Backspace', 'Delete'].includes(event.key) && !event.ctrlKey && !event.metaKey) {
    const selection = capture()
    const start = Math.min(selection.anchor, selection.focus), end = Math.max(selection.anchor, selection.focus)
    const from = event.key === 'Backspace' ? start - 1 : start
    const adjacent = sliceParts(current.value, Math.max(0, from), from + 1)
    if (start === end && adjacent[0]?.type === 'reference') {
      event.preventDefault(); replace(from, from + 1, []); closeMenu()
    }
  }
}
function outside(event: PointerEvent) {
  if (open.value && event.target instanceof Node && !menu.value?.contains(event.target) && !editor.value?.contains(event.target)) closeMenu()
}
watch(() => props.nodeId, () => {
  current.value = draftParts({ prompt: props.modelValue, promptParts: props.parts })
  undo = []; redo = []; before = undefined; composing = false; bookmark = { anchor: 0, focus: 0 }; closeMenu()
  void nextTick(() => { render(); focus() })
}, { immediate: true })
watch(() => [props.modelValue, props.parts], () => {
  const parts = draftParts({ prompt: props.modelValue, promptParts: props.parts })
  if (JSON.stringify(parts) !== JSON.stringify(current.value)) { current.value = parts; render() }
})
watch(() => props.candidates, () => {
  editor.value?.querySelectorAll<HTMLElement>('[data-prompt-reference]').forEach(paintChip)
  if (open.value) void nextTick(positionMenu)
}, { deep: true })
onMounted(() => { document.addEventListener('pointerdown', outside, true); window.addEventListener('resize', positionMenu) })
onBeforeUnmount(() => { document.removeEventListener('pointerdown', outside, true); window.removeEventListener('resize', positionMenu) })
defineExpose({ focus, capture, restore, closeMenu })
</script>

<template>
  <div class="prompt-editor-wrap">
    <div ref="editor" class="prompt-input" contenteditable="true" role="textbox" aria-multiline="true"
      :aria-label="label" :data-placeholder="placeholder" :aria-expanded="open" aria-haspopup="dialog"
      :aria-controls="open ? `mentions-${nodeId}` : undefined" spellcheck="false"
      @beforeinput="beforeInput" @input="onInput" @keydown="keydown"
      @keyup="(event) => { if (!open && ['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) capture() }"
      @pointerdown="lastInputType = ''" @pointerup="capture" @click="refreshTrigger" @scroll="positionMenu" @blur="capture"
      @compositionstart="compositionStart" @compositionend="compositionEnd" @paste="paste" @copy="copy($event)" @cut="copy($event, true)"
      @drop.prevent @dragstart.prevent></div>
    <div v-if="open" :id="`mentions-${nodeId}`" ref="menu" class="mention-menu" :class="{ 'has-category': category && !compact }" :style="menuStyle"
      role="dialog" aria-label="素材引用菜单" @pointerdown.stop @mousedown.stop @wheel.stop @keydown="menuKey">
      <div class="mention-main" v-show="!compact || !category">
        <label class="mention-search"><span aria-hidden="true">⌕</span><input ref="search" v-model="query" aria-label="搜索素材" placeholder="搜索素材…" autocomplete="off" /></label>
        <div class="mention-scroll">
          <p class="mention-heading">已引用</p>
          <p v-if="!selected.length" class="mention-empty">{{ query ? '没有匹配的已引用素材' : '尚未引用图片或视频' }}</p>
          <button v-for="item in selected" :key="item.id" type="button" class="mention-item" :data-active="activeKey === `selected:${item.id}`" :aria-disabled="!!item.error"
            :title="item.error || item.name" :aria-label="`引用 ${item.name}`" @mousedown.prevent @click="choose(item)">
            <img v-if="item.kind === 'image' && item.url" :src="item.url" alt="" /><span v-else class="mention-kind">▷</span>
            <span class="mention-copy"><span>{{ item.name }}</span><small v-if="item.error">{{ item.error }}</small></span>
          </button>
          <p class="mention-heading material-heading">素材引用</p>
          <button v-for="kind in (['image', 'video'] as const)" :key="kind" type="button" class="mention-item mention-category"
            :class="{ 'is-category': category === kind }" :data-active="activeKey === kind" :aria-label="`${kind === 'image' ? '图片' : '视频'}素材`" :aria-expanded="category === kind"
            @mousedown.prevent @pointerenter="!compact && ($event.pointerType === 'mouse') && showCategory(kind)" @click="showCategory(kind)">
            <span class="mention-kind">{{ kind === 'image' ? '▧' : '▷' }}</span><span>{{ kind === 'image' ? '图片' : '视频' }}</span><span class="mention-chevron">›</span>
          </button>
        </div>
      </div>
      <div v-if="category" class="mention-submenu">
        <div class="mention-subhead"><button v-if="compact" type="button" aria-label="返回素材分类" @click="category = null">‹</button><span>{{ category === 'image' ? '图片' : '视频' }}素材</span><span class="mention-count">{{ resources.length }}</span></div>
        <div class="mention-scroll">
          <p v-if="!resources.length" class="mention-empty">{{ query ? '没有匹配的素材' : '当前画布暂无此类素材' }}</p>
          <button v-for="item in resources" :key="item.id" type="button" class="mention-item" :data-active="activeKey === `asset:${item.id}`" :aria-disabled="!!item.error"
            :title="item.error || item.name" :aria-label="`引用 ${item.name}`" @mousedown.prevent @click="choose(item)">
            <img v-if="item.kind === 'image' && item.url" :src="item.url" alt="" />
            <span v-else class="mention-kind">▷</span>
            <span class="mention-copy"><span>{{ item.name }}</span><small v-if="item.error">{{ item.error }}</small></span>
            <span v-if="item.selected" class="mention-check" aria-label="已引用">✓</span>
          </button>
        </div>
      </div>
      <p v-if="menuError" class="mention-feedback" role="status">{{ menuError }}</p>
    </div>
  </div>
</template>

<style scoped>
.prompt-editor-wrap { flex: 1; min-height: 40px; margin-top: 12px; display: flex; }
.prompt-input { flex: 1; min-width: 0; min-height: 0; overflow-y: auto; padding: 2px 0; outline: none; color: #e4e4e4; font: inherit; font-size: 14px; line-height: 1.9; white-space: pre-wrap; overflow-wrap: anywhere; scrollbar-width: thin; scrollbar-color: #555 transparent; }
.prompt-input:empty::before { content: attr(data-placeholder); color: #919191; pointer-events: none; }
.prompt-input :deep(.prompt-chip) { display: inline-flex; align-items: center; gap: 4px; max-width: 230px; height: 24px; padding: 0 5px 0 3px; margin: 0 2px; vertical-align: text-bottom; border: 1px solid #555; border-radius: 5px; background: #3b3b3b; color: #ededed; font-size: 12px; line-height: 22px; white-space: nowrap; user-select: all; }
.prompt-input :deep(.prompt-chip > span:last-child) { overflow: hidden; text-overflow: ellipsis; }
.prompt-input :deep(.prompt-chip img) { width: 17px; height: 17px; border-radius: 3px; object-fit: cover; pointer-events: none; }
.prompt-input :deep(.chip-icon) { color: #bcbcbc; }
.prompt-input :deep(.chip-icon::before) { content: '▧'; }
.prompt-input :deep(.chip-icon[data-kind="video"]::before) { content: '▷'; }
.prompt-input :deep(.prompt-chip.is-invalid) { color: #e6c295; border-color: #8b6c47; background: #44382a; }
.mention-menu { position: fixed; z-index: 45; display: flex; overflow: hidden; padding: 6px; border: 1px solid #454545; border-radius: 14px; background: #272727; box-shadow: 0 10px 35px #0005; color: #eee; font-size: 13px; }
.mention-main, .mention-submenu { display: flex; flex-direction: column; min-width: 0; width: 100%; }
.has-category .mention-main { width: 218px; flex: none; }
.mention-submenu { border-left: 1px solid #404040; margin-left: 6px; padding-left: 6px; }
.mention-main[style*="display: none"] + .mention-submenu { border-left: 0; padding-left: 0; margin-left: 0; }
.mention-search { display: flex; flex: none; align-items: center; gap: 6px; height: 32px; padding: 0 8px; margin: 0 0 7px; border: 1px solid #505050; border-radius: 9px; color: #a0a0a0; }
.mention-search > span { font-size: 21px; }
.mention-search:focus-within { border-color: #929292; }
.mention-search input { width: 100%; min-width: 0; background: none; outline: none; border: none; color: #eee; font: inherit; }
.mention-scroll { min-height: 0; overflow-y: auto; overscroll-behavior: contain; scrollbar-width: thin; scrollbar-color: #555 transparent; }
.mention-heading { color: #999; margin: 5px 7px 6px; font-size: 12px; }
.material-heading { margin-top: 13px; }
.mention-empty { color: #959595; margin: 10px 7px 14px; font-size: 12px; line-height: 1.6; }
.mention-item { display: flex; align-items: center; width: 100%; gap: 8px; min-height: 36px; padding: 7px; background: none; border: none; border-radius: 8px; color: inherit; font: inherit; text-align: left; cursor: pointer; }
.mention-item:hover, .mention-item[data-active="true"], .mention-item.is-category { background: #3b3b3b; }
.mention-item:focus-visible { outline: 2px solid #9badc3; outline-offset: -2px; }
.mention-item[aria-disabled="true"] { color: #888; cursor: not-allowed; }
.mention-item img, .mention-kind { width: 20px; height: 20px; flex: none; border-radius: 4px; object-fit: cover; }
.mention-kind { display: grid; place-items: center; font-size: 19px; }
.mention-copy { display: flex; flex-direction: column; min-width: 0; gap: 3px; }
.mention-copy > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mention-copy small { font-size: 10px; color: #ad977e; line-height: 1.4; }
.mention-chevron { margin-left: auto; color: #999; }
.mention-check { margin-left: auto; color: #afbdad; }
.mention-subhead { display: flex; gap: 8px; align-items: center; flex: none; height: 32px; padding: 0 7px; color: #aaa; font-size: 12px; }
.mention-count { margin-left: auto; font-variant-numeric: tabular-nums; color: #777; }
.mention-subhead button { padding: 0 7px; border: 0; background: none; color: #ddd; font-size: 22px; cursor: pointer; }
.mention-feedback { position: absolute; left: 6px; right: 6px; bottom: 0; background: #403429; color: #ebcdab; padding: 8px; margin: 0; font-size: 11px; }
</style>
