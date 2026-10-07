<script setup lang="ts">
import { computed, ref } from 'vue'
import { auth, login } from '../auth/session'
const props = defineProps<{ resumeEmail?: string; createAfterLogin?: boolean }>()
const email = ref(props.resumeEmail || '')
const password = ref('')
const visible = ref(false)
const busy = ref(false)
const error = ref('')
const title = computed(() => props.resumeEmail ? '继续你的创作' : props.createAfterLogin ? '登录后创建画布' : '进入你的工作空间')
async function submit() {
  if (busy.value) return
  // Count Unicode characters like the backend; never truncate a pasted password.
  const length = Array.from(password.value).length
  if (length < 6 || length > 20 || !password.value.trim()) {
    error.value = '密码需为 6～20 个字符，且不能全为空白。'
    return
  }
  busy.value = true; error.value = ''
  try { await login(email.value.trim(), password.value); password.value = '' }
  catch (cause) { error.value = cause instanceof Error ? cause.message : '无法连接登录服务，请稍后重试。' }
  finally { busy.value = false }
}
</script>

<template>
  <main class="login-page" :class="{ 'login-resume': resumeEmail }">
    <a class="brand login-brand" href="/" aria-label="帧间首页">
      <img class="brand-mark" src="/favicon.svg" alt="" width="34" height="34" /><span class="brand-name">帧间</span>
    </a>
    <div class="login-layout">
      <div class="login-intro" aria-hidden="true">
        <div class="login-viewfinder"><span class="viewfinder-corner" /><div class="login-frame frame-back" /><div class="login-frame frame-front"><span>帧间</span><i /></div><span class="viewfinder-corner corner-end" /></div>
        <p class="login-statement">让想法，<br />有地方展开。</p>
        <p class="login-caption">图片、视频与灵感，都在你的画布里。</p>
      </div>
      <section class="login-card" aria-labelledby="login-title">
        <p class="login-eyebrow">你的创作空间</p>
        <h1 id="login-title">{{ title }}</h1>
        <p class="login-description">{{ resumeEmail ? auth.error : '使用邮箱和密码继续。' }}</p>
        <form @submit.prevent="submit">
          <label for="login-email">邮箱</label>
          <input id="login-email" v-model="email" name="email" type="email" autocomplete="username" autocapitalize="none" spellcheck="false" maxlength="254" placeholder="you@example.com" required :readonly="!!resumeEmail" :disabled="busy" />
          <label for="login-password">密码</label>
          <div class="login-password">
            <input id="login-password" v-model="password" name="password" :type="visible ? 'text' : 'password'" autocomplete="current-password" placeholder="6～20 个字符" required :disabled="busy" aria-describedby="password-help" />
            <button type="button" :aria-label="visible ? '隐藏密码' : '显示密码'" :aria-pressed="visible" @click="visible = !visible">{{ visible ? '隐藏' : '显示' }}</button>
          </div>
          <p id="password-help" class="login-help">密码长度为 6～20 个字符，支持粘贴密码。</p>
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="login-submit" type="submit" :disabled="busy">{{ busy ? '正在进入…' : resumeEmail ? '重新登录并继续' : '继续' }}<span v-if="!busy" aria-hidden="true">→</span></button>
        </form>
        <p v-if="!resumeEmail" class="login-registration">未注册的邮箱将自动创建账号。<br />本次输入的密码将作为登录密码。</p>
        <p class="login-recovery">暂不支持找回密码，请妥善保存。</p>
      </section>
    </div>
    <p class="login-footer">帧间 · 从一个想法开始</p>
  </main>
</template>
