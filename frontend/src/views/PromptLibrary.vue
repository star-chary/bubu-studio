<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { getPrompts, type LibraryPrompt } from '../api/promptLibrary'
import PromptCard from '../components/PromptCard.vue'

const items = ref<LibraryPrompt[]>([])
const query = ref(''), appliedQuery = ref(''), error = ref('')
const loading = ref(false), hasMore = ref(false)
let controller: AbortController | undefined
let request = 0
let nextOffset = 0
async function load(append = false) {
  controller?.abort()
  controller = new AbortController()
  const current = ++request
  loading.value = true; error.value = ''
  try {
    const offset = append ? nextOffset : 0
    const result = await getPrompts(appliedQuery.value, offset, controller.signal)
    if (current !== request) return
    if (append) {
      const existing = new Set(items.value.map((item) => item.id))
      items.value.push(...result.prompts.filter((item) => !existing.has(item.id)))
    } else items.value = result.prompts
    nextOffset = offset + result.prompts.length
    hasMore.value = result.hasMore
  } catch (cause) {
    if (current === request) error.value = cause instanceof Error && cause.name !== 'AbortError' ? cause.message : '提示词加载失败，请重试。'
  } finally { if (current === request) loading.value = false }
}
function search() { appliedQuery.value = query.value.trim(); items.value = []; hasMore.value = false; void load() }
onMounted(() => void load())
onBeforeUnmount(() => { request++; controller?.abort() })
</script>

<template>
  <main class="prompt-page">
    <div class="prompt-content">
      <div class="page-title"><h1>提示词</h1><p>收集灵感，找到下一张想创作的图片。</p></div>
      <form class="search" role="search" @submit.prevent="search">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4 4"/></svg>
        <label class="sr-only" for="prompt-search">搜索提示词</label>
        <input id="prompt-search" v-model="query" maxlength="100" placeholder="搜索提示词或标题" autocomplete="off" />
        <button type="submit" :disabled="loading">搜索</button>
      </form>
      <div class="collection-heading"><h2>{{ appliedQuery ? '搜索结果' : '全部提示词' }}</h2></div>
      <div v-if="error" class="state" role="alert"><p>{{ error }}</p><button type="button" @click="load()">重新加载</button></div>
      <p v-else-if="loading && !items.length" class="state" role="status">正在加载提示词…</p>
      <div v-else-if="!items.length" class="state"><p>{{ appliedQuery ? '没有找到匹配的提示词。' : '还没有提示词。' }}</p></div>
      <div v-if="items.length" class="prompt-grid"><PromptCard v-for="item in items" :key="item.id" :prompt="item" /></div>
      <button v-if="hasMore && !error" class="load-more" type="button" :disabled="loading" @click="load(true)">{{ loading ? '正在加载…' : '加载更多' }}</button>
    </div>
  </main>
</template>

<style scoped>
.prompt-page { width: 100%; height: 100%; overflow-y: auto; background: #212121; }
.prompt-content { max-width: 1360px; margin: 0 auto; padding: 56px clamp(24px, 4vw, 64px) 80px; }
.page-title { margin-bottom: 32px; }
.page-title h1 { margin: 0 0 12px; color: #f2f2f2; font-size: 34px; font-weight: 600; letter-spacing: -.025em; }
.page-title p { margin: 0; color: #a5a5a5; font-size: 14px; }
.search { display: flex; align-items: center; gap: 12px; max-width: 560px; height: 48px; padding: 0 7px 0 15px; border: 1px solid #454545; border-radius: 10px; background: #2b2b2b; }
.search:focus-within { border-color: #aaa; }
.search svg { width: 19px; height: 19px; color: #a8a8a8; flex: none; }
.search input { flex: 1; min-width: 0; border: 0; outline: 0; background: transparent; color: #eee; font: inherit; font-size: 14px; }
.search input::placeholder { color: #939393; }
.search button, .load-more, .state button { padding: 8px 14px; border: 1px solid #555; border-radius: 7px; background: #353535; color: #eee; cursor: pointer; font-size: 13px; }
.search button:hover, .load-more:hover, .state button:hover { background: #454545; }
.collection-heading { display: flex; align-items: center; margin: 42px 0 20px; }
.collection-heading h2 { margin: 0; color: #ededed; font-size: 18px; font-weight: 600; }
.prompt-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 22px; align-items: start; }
.state { margin: 0; padding: 34px 0; color: #aaa; font-size: 14px; }
.state p { margin: 0 0 16px; }
.load-more { display: block; margin: 32px auto 0; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@media (max-width: 900px) { .prompt-grid { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 600px) { .prompt-content { padding: 35px 18px 56px; } .page-title h1 { font-size: 29px; } .collection-heading { margin-top: 32px; } }
</style>
