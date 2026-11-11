<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api, formatDate } from './api'

interface PromptImage { id: string; url: string }
interface Prompt { id: string; title: string; content: string; images: PromptImage[]; createdAt: string; updatedAt: string }
interface Editor { id: string; title: string; content: string; images: PromptImage[]; persisted: boolean; originalTitle: string; originalContent: string }
interface PendingImage { id: string; file: File; preview: string }
const items = ref<Prompt[]>([]), query = ref(''), appliedQuery = ref(''), error = ref(''), notice = ref('')
const loading = ref(false), hasMore = ref(false), page = ref(0), busy = ref(false), editorError = ref('')
const editor = ref<Editor | null>(null), pending = ref<PendingImage[]>([])
const pageSize = 20
let version = 0
async function load() {
  const current = ++version; loading.value = true; error.value = ''
  try {
    const params = new URLSearchParams({ q: appliedQuery.value, limit: String(pageSize), offset: String(page.value * pageSize) })
    const result = await api<{ prompts: Prompt[]; hasMore: boolean }>(`/api/admin/prompts?${params}`)
    if (current !== version) return
    items.value = result.prompts; hasMore.value = result.hasMore
  } catch (cause) { if (current === version) error.value = cause instanceof Error ? cause.message : '提示词列表加载失败。' }
  finally { if (current === version) loading.value = false }
}
function search() { appliedQuery.value = query.value.trim(); page.value = 0; void load() }
function turn(amount: number) { page.value += amount; void load() }
function clearPending() { for (const image of pending.value) URL.revokeObjectURL(image.preview); pending.value = [] }
function open(item?: Prompt) {
  clearPending(); editorError.value = ''; notice.value = ''
  editor.value = item
    ? { id: item.id, title: item.title, content: item.content, images: [...item.images], persisted: true, originalTitle: item.title, originalContent: item.content }
    : { id: crypto.randomUUID(), title: '', content: '', images: [], persisted: false, originalTitle: '', originalContent: '' }
}
function close() {
  const value = editor.value
  if (value && (value.title !== value.originalTitle || value.content !== value.originalContent || pending.value.length) && !window.confirm('还有未保存的内容，确定返回列表吗？')) return
  clearPending(); editor.value = null; editorError.value = ''; void load()
}
function chooseImages(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  const rejected = files.filter((file) => !file.type.startsWith('image/') || file.size > 20 * 1024 * 1024 || file.size === 0)
  for (const file of files.filter((file) => !rejected.includes(file))) pending.value.push({ id: crypto.randomUUID(), file, preview: URL.createObjectURL(file) })
  editorError.value = rejected.length ? `${rejected.length} 张文件不是有效图片或超过 20 MiB，请重新选择。` : ''
}
function removePending(id: string) {
  const found = pending.value.find((image) => image.id === id)
  if (found) URL.revokeObjectURL(found.preview)
  pending.value = pending.value.filter((image) => image.id !== id)
}
async function save() {
  const value = editor.value
  if (!value || busy.value) return
  busy.value = true; editorError.value = ''
  let textSaved = false
  try {
    const title = value.title.trim(), content = value.content.trim()
    if (!title || !content) throw new Error('请填写标题和提示词内容。')
    if (value.persisted) await api(`/api/admin/prompts/${value.id}`, { method: 'PUT', body: JSON.stringify({ title, content }) })
    else {
      await api('/api/admin/prompts', { method: 'POST', body: JSON.stringify({ id: value.id, title, content }) })
      value.persisted = true
    }
    textSaved = true
    value.title = title; value.content = content; value.originalTitle = title; value.originalContent = content
    for (const image of [...pending.value]) {
      const saved = await api<PromptImage>(`/api/admin/prompts/${value.id}/images/${image.id}`, { method: 'PUT', body: image.file }, 150000)
      value.images.push(saved); removePending(image.id)
    }
    editor.value = null; notice.value = '提示词已保存。'; page.value = 0; await load()
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : '保存失败，请重试。'
    editorError.value = textSaved && pending.value.length ? `提示词已保存，仍有 ${pending.value.length} 张图片待上传：${message} 请点击保存重试。` : message
  } finally { busy.value = false }
}
async function deleteImage(image: PromptImage) {
  const value = editor.value
  if (!value || busy.value || !window.confirm('确定删除这张图片吗？删除后前台将不再显示。')) return
  busy.value = true; editorError.value = ''
  try {
    await api(`/api/admin/prompts/${value.id}/images/${image.id}`, { method: 'DELETE' })
    value.images = value.images.filter((item) => item.id !== image.id)
  } catch (cause) { editorError.value = cause instanceof Error ? cause.message : '删除图片失败，请重试。' }
  finally { busy.value = false }
}
async function deletePrompt(item: Prompt) {
  if (busy.value || !window.confirm(`确定删除“${item.title}”及其全部图片吗？删除后前台将不再显示。`)) return
  busy.value = true; error.value = ''; notice.value = ''
  try {
    await api(`/api/admin/prompts/${item.id}`, { method: 'DELETE' })
    if (items.value.length === 1 && page.value > 0) page.value--
    notice.value = '提示词已删除。'; await load()
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '删除失败，请重试。' }
  finally { busy.value = false }
}
onMounted(() => void load())
onBeforeUnmount(() => { version++; clearPending() })
</script>

<template>
  <main class="users-page prompts-admin">
    <template v-if="!editor">
      <div class="page-heading"><div><p class="eyebrow">内容管理</p><h1>提示词管理</h1><p class="muted">管理首页展示的提示词和图片。</p></div><button class="primary" type="button" @click="open()">＋ 新增提示词</button></div>
      <p v-if="notice" class="notice success" role="status">{{ notice }}</p>
      <section class="user-card" aria-label="提示词列表">
        <div class="table-toolbar"><div class="table-title">全部提示词</div><form class="search-form" @submit.prevent="search"><label for="admin-prompt-search" class="sr-only">搜索提示词</label><input id="admin-prompt-search" v-model="query" maxlength="100" placeholder="搜索标题或提示词" /><button class="secondary" :disabled="loading">搜索</button></form></div>
        <div v-if="error" class="empty-state" role="alert"><strong>提示词列表暂时不可用</strong><p>{{ error }}</p><button class="secondary" @click="load">重试</button></div>
        <div v-else-if="loading" class="empty-state" role="status">正在读取提示词…</div>
        <div v-else-if="!items.length" class="empty-state"><strong>{{ appliedQuery ? '没有找到匹配的提示词' : '还没有提示词' }}</strong><p>{{ appliedQuery ? '试试其他关键词。' : '点击新增提示词，保存文字和图片。' }}</p></div>
        <div v-else class="table-scroll"><table><thead><tr><th>提示词</th><th>图片</th><th>更新时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td><div class="prompt-title">{{ item.title }}</div><p class="prompt-summary">{{ item.content }}</p></td><td><div class="image-cell"><img v-if="item.images[0]" :src="item.images[0].url" alt="" /><span>{{ item.images.length }} 张</span></div></td><td class="date-cell">{{ formatDate(item.updatedAt) }}</td><td><div class="row-actions"><button class="text-button" type="button" :disabled="busy" @click="open(item)">编辑</button><button class="text-button danger" type="button" :disabled="busy" @click="deletePrompt(item)">删除</button></div></td></tr></tbody></table></div>
        <footer class="table-footer"><span>按创建时间排序</span><nav aria-label="提示词分页"><button class="secondary" :disabled="loading || page === 0" @click="turn(-1)">上一页</button><span>第 {{ page + 1 }} 页</span><button class="secondary" :disabled="loading || !hasMore" @click="turn(1)">下一页</button></nav></footer>
      </section>
    </template>
    <template v-else>
      <div class="page-heading"><div><p class="eyebrow">内容管理 / 提示词</p><h1>{{ editor.persisted ? '编辑提示词' : '新增提示词' }}</h1><p class="muted">保存后，文字和图片会在前台公开展示。</p></div><button class="secondary" type="button" :disabled="busy" @click="close">返回列表</button></div>
      <form class="user-card prompt-editor" @submit.prevent="save">
        <label for="prompt-title">标题</label><input id="prompt-title" v-model="editor.title" maxlength="100" required :disabled="busy" placeholder="给这组图片起个名字" />
        <label for="prompt-content">提示词内容</label><textarea id="prompt-content" v-model="editor.content" maxlength="20000" required :disabled="busy" rows="9" placeholder="粘贴完整的图片提示词"></textarea>
        <div class="image-section-heading"><div><strong>图片</strong><p class="field-help">可一次选择多张，每张不超过 20 MiB。图片将按上传顺序展示。</p></div><label class="secondary file-button" :class="{ disabled: busy }">选择图片<input type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/avif,image/bmp" multiple :disabled="busy" @change="chooseImages" /></label></div>
        <div v-if="editor.images.length || pending.length" class="image-grid">
          <div v-for="image in editor.images" :key="image.id" class="image-item"><img :src="image.url" alt="已上传的图片" /><button type="button" :disabled="busy" aria-label="删除已上传图片" @click="deleteImage(image)">删除</button></div>
          <div v-for="image in pending" :key="image.id" class="image-item"><img :src="image.preview" :alt="image.file.name" /><span>待上传</span><button type="button" :disabled="busy" :aria-label="`移除待上传图片 ${image.file.name}`" @click="removePending(image.id)">移除</button></div>
        </div>
        <p v-else class="no-images">暂未选择图片。</p>
        <p v-if="editorError" class="notice error" role="alert">{{ editorError }}</p>
        <div class="button-row"><button class="secondary" type="button" :disabled="busy" @click="close">取消</button><button class="primary" type="submit" :disabled="busy">{{ busy ? '正在保存…' : '保存提示词' }}</button></div>
      </form>
    </template>
  </main>
</template>

<style scoped>
.prompt-title { max-width: 380px; overflow: hidden; color: #2b3a52; font-size: 13px; font-weight: 600; text-overflow: ellipsis; }
.prompt-summary { max-width: 380px; overflow: hidden; color: #7b8799; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.image-cell { display: flex; align-items: center; gap: 10px; color: #738096; }
.image-cell img { width: 48px; height: 42px; border-radius: 5px; object-fit: cover; background: #eef1f6; }
.danger { color: #ad4b4b; }
.prompt-editor { max-width: 900px; padding: 28px; }
.prompt-editor > label { display: block; margin: 0 0 9px; color: #35445e; font-size: 13px; font-weight: 600; }
.prompt-editor > input, .prompt-editor > textarea { width: 100%; margin-bottom: 23px; padding: 11px 13px; border: 1px solid #dce3ee; border-radius: 7px; background: #fbfcfe; color: #263550; }
.prompt-editor > textarea { resize: vertical; line-height: 1.6; }
.image-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 15px; padding-top: 2px; }
.image-section-heading strong { font-size: 13px; }.field-help { font-size: 12px; }
.file-button { position: relative; flex: none; cursor: pointer; }.file-button:focus-within { outline: 2px solid #4467d4; outline-offset: 3px; }.file-button.disabled { opacity: .48; cursor: not-allowed; }.file-button input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.image-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 12px; margin: 18px 0 22px; }
.image-item { position: relative; aspect-ratio: 1; overflow: hidden; border-radius: 7px; background: #edf0f5; }
.image-item img { width: 100%; height: 100%; object-fit: cover; }.image-item button, .image-item span { position: absolute; bottom: 7px; padding: 5px 7px; border-radius: 5px; background: #182238c9; color: white; font-size: 11px; }.image-item button { right: 7px; }.image-item span { left: 7px; }
.no-images { padding: 25px 0; color: #8b97ab; font-size: 13px; }
@media (max-width: 600px) { .prompt-editor { padding: 20px; } .image-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } .image-section-heading { align-items: flex-start; flex-direction: column; } }
</style>
