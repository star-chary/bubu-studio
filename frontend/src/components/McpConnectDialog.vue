<script setup lang="ts">
import { onMounted, shallowRef, useTemplateRef } from 'vue'

const emit = defineEmits<{ close: [] }>()
const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const copyStatus = shallowRef('')
const mcpUrl = `${window.location.origin}/mcp`
const prompt = `请帮我连接 Bubu 画布 MCP：
${mcpUrl}

请使用 OAuth 方式连接，并引导我使用 Bubu 普通用户账号登录和授权。连接后请先查看可用工具，再根据我的要求操作画布。

修改画布前，请提醒我先保存并离开画布；提交图片或视频生成前，请先告知积分报价并获得我的确认。`

async function copyPrompt() {
  try {
    await navigator.clipboard.writeText(prompt)
    copyStatus.value = '已复制'
  } catch {
    copyStatus.value = '复制失败，请手动选择上方文字复制。'
  }
}

onMounted(() => dialog.value?.showModal())
</script>

<template>
  <dialog ref="dialog" class="mcp-dialog" aria-labelledby="mcp-dialog-title" @close="emit('close')" @click.self="dialog?.close()">
    <div class="dialog-heading">
      <div>
        <p class="dialog-eyebrow">AI 应用连接</p>
        <h2 id="mcp-dialog-title">连接 Bubu 画布</h2>
      </div>
      <button type="button" class="close-button" aria-label="关闭 MCP 连接" @click="dialog?.close()">×</button>
    </div>
    <p class="dialog-description">复制下方提示词，发送给支持 MCP 的 AI 应用，按提示完成登录和授权。</p>
    <label class="prompt-label" for="mcp-prompt">接入提示词</label>
    <textarea id="mcp-prompt" class="prompt-content" :value="prompt" readonly spellcheck="false" />
    <p class="dialog-note">使用你的 Bubu 账号授权即可，无需将账号密码发送给 AI。</p>
    <div class="dialog-actions">
      <span class="copy-status" role="status">{{ copyStatus }}</span>
      <button type="button" class="copy-button" @click="copyPrompt">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></svg>
        复制提示词
      </button>
    </div>
  </dialog>
</template>

<style scoped>
.mcp-dialog { width: min(520px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); margin: auto; padding: 28px; overflow-y: auto; border: 1px solid #4d4d4f; border-radius: 14px; background: #242426; color: #f1f1ef; box-shadow: 0 24px 80px #0006; }
.mcp-dialog::backdrop { background: #080808c2; }
.dialog-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.dialog-eyebrow { margin: 0 0 8px; color: #929292; font-size: 11px; font-weight: 600; }
.dialog-heading h2 { margin: 0; font-size: 20px; font-weight: 600; line-height: 1.4; }
.close-button { display: grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 7px; background: transparent; color: #aaa; font-size: 25px; line-height: 1; cursor: pointer; }
.close-button:hover { background: #353537; color: #f1f1ef; }
.dialog-description, .dialog-note { color: #aaa; font-size: 13px; line-height: 1.7; }
.dialog-description { margin: 14px 0 24px; }
.prompt-label { display: block; margin-bottom: 10px; color: #e6e6e4; font-size: 13px; font-weight: 600; }
.prompt-content { display: block; width: 100%; min-height: 200px; padding: 15px; resize: vertical; border: 1px solid #49494b; border-radius: 9px; background: #19191b; color: #d8d8d6; font: inherit; font-size: 13px; line-height: 1.7; }
.prompt-content:focus-visible { outline: 2px solid #dededb; outline-offset: 2px; }
.dialog-note { margin: 12px 0 0; font-size: 12px; }
.dialog-actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 24px; }
.copy-status { color: #bcbcb9; font-size: 12px; line-height: 1.5; }
.copy-button { display: inline-flex; align-items: center; justify-content: center; gap: 8px; min-height: 40px; padding: 9px 15px; border: 1px solid #f0efeb; border-radius: 9px; background: #f0efeb; color: #161617; font-size: 13px; font-weight: 600; white-space: nowrap; cursor: pointer; }
.copy-button:hover { border-color: #d8d8d4; background: #d8d8d4; }
.copy-button svg { width: 16px; height: 16px; }
.close-button:focus-visible, .copy-button:focus-visible { outline: 2px solid #ededed; outline-offset: 3px; }
@media (max-width: 600px) { .mcp-dialog { padding: 22px; } .prompt-content { min-height: 270px; } }
@media (max-width: 360px) { .prompt-content { min-height: 300px; } }
</style>
