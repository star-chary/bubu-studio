<script setup lang="ts">
import type { GenerationTask } from '../api/persistence'
defineProps<{ tasks: GenerationTask[]; hasMore: boolean; loading: boolean; error: string }>()
defineEmits<{ close: []; more: []; refresh: []; retryStorage: [id: string] }>()
const labels: Record<GenerationTask['status'], string> = { queued: '等待执行', preparing: '准备素材', running: '生成中', submitting: '正在提交', storage_failed: '保存失败', saving: '正在保存素材', succeeded: '已完成', failed: '失败', interrupted: '已中断' }
const creditLabels: Record<NonNullable<GenerationTask['creditStatus']>, string> = { legacy: '旧任务', reserved: '已冻结', review: '待核查', settled: '已扣除', released: '已退回' }
const date = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
</script>
<template>
  <aside class="task-history" aria-label="生成任务记录" @pointerdown.stop @wheel.stop @keydown.esc="$emit('close')">
    <header><h2>生成任务</h2><button type="button" @click="$emit('refresh')" :disabled="loading">刷新</button><button type="button" aria-label="关闭任务记录" @click="$emit('close')">×</button></header>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="!tasks.length">{{ loading ? '正在读取…' : '还没有生成任务' }}</p>
    <ol>
      <li v-for="task in tasks" :key="task.id">
        <div class="task-heading"><strong :class="task.status">{{ labels[task.status] }}</strong><time>{{ date(task.createdAt) }}</time></div>
        <p class="task-prompt">{{ task.input.prompt }}</p>
        <dl><dt>任务 ID</dt><dd>{{ task.id }}</dd><dt>模型</dt><dd>{{ task.input.model }}</dd><dt>参考素材</dt><dd>{{ task.input.references?.length ?? task.input.referenceKeys?.length ?? 0 }} 项</dd><template v-if="task.creditStatus && task.creditStatus !== 'legacy'"><dt>测试积分</dt><dd>{{ task.creditPoints }} 分 · {{ creditLabels[task.creditStatus] }}</dd></template><template v-if="task.compiledPrompt"><dt>实际发送提示词</dt><dd>{{ task.compiledPrompt }}</dd></template></dl>
        <p v-if="task.error" class="task-error">{{ task.error.message }}</p>
        <p v-if="task.pollingError" class="task-error">{{ task.pollingError.message }}</p>
        <button v-if="task.status === 'storage_failed'" type="button" @click="$emit('retryStorage', task.id)">重试保存视频</button>
        <p v-if="task.result?.storageError" class="task-error">{{ task.result.storageError }}</p>
        <a v-if="task.result?.url" :href="task.result.url" target="_blank" rel="noopener noreferrer">查看结果</a>
      </li>
    </ol>
    <button v-if="hasMore" type="button" :disabled="loading" @click="$emit('more')">{{ loading ? '正在读取…' : '加载更多' }}</button>
  </aside>
</template>
<style scoped>
.task-history { position: absolute; z-index: 45; top: 78px; right: 20px; width: min(380px, calc(100% - 40px)); max-height: calc(100% - 110px); overflow: auto; padding: 18px; background: #22252b; border: 1px solid #42454e; border-radius: 14px; color: #daddE5; box-shadow: 0 15px 45px #0006; font-size: 13px; }
header { display: flex; align-items: center; gap: 12px; } h2 { flex: 1; margin: 0; font-size: 16px; }
button { border: 1px solid #525762; border-radius: 6px; background: #30343c; padding: 5px 9px; color: inherit; cursor: pointer; } button:disabled { opacity: .5; }
ol { padding: 0; list-style: none; } li { border-top: 1px solid #40434b; padding: 16px 0; }
.task-heading { display: flex; justify-content: space-between; gap: 10px; } time { color: #a2a7b3; font-size: 11px; }
.succeeded { color: #8bcbb0; } .failed,.interrupted,.task-error { color: #e5b092; } .running,.saving,.queued { color: #a1bffc; }
.task-prompt { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.6; } dl { font-size: 11px; color: #a2a7b3; } dt { margin-top: 7px; } dd { margin: 3px 0 0; overflow-wrap: anywhere; } a { color: #a1bffc; } .task-error { line-height: 1.6; }
</style>
