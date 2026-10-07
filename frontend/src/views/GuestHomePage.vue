<script setup lang="ts">
defineProps<{ serviceError?: string; sessionNotice?: string }>()
defineEmits<{ login: []; create: []; retry: [] }>()
</script>

<template>
  <div class="guest-home">
    <header class="guest-header">
      <a class="brand" href="/" aria-label="帧间首页">
        <img class="brand-mark" src="/favicon.svg" alt="" width="34" height="34" />
        <span class="brand-name">帧间</span>
      </a>
      <button class="guest-sign-in" type="button" @click="$emit('login')">登录 / 注册</button>
    </header>
    <main class="guest-main">
      <div class="guest-copy">
        <p class="guest-eyebrow">你的创作空间</p>
        <h1>让想法，<br />有地方展开。</h1>
        <p class="guest-description">从一张空白画布开始，整理图片、视频与灵感。</p>
        <button class="guest-create" type="button" @click="$emit('create')">创建画布 <span aria-hidden="true">↗</span></button>
        <p class="guest-hint">创建前需要登录。未注册的邮箱可直接创建账号。</p>
        <div v-if="serviceError" class="guest-notice" role="alert">
          <p>登录服务暂时不可用：{{ serviceError }}</p>
          <button type="button" @click="$emit('retry')">重新连接</button>
        </div>
        <p v-else-if="sessionNotice" class="guest-session-notice" role="status">{{ sessionNotice }}</p>
      </div>
      <div class="guest-art" aria-hidden="true">
        <div class="guest-art-grid" />
        <div class="guest-art-frame frame-one" />
        <div class="guest-art-frame frame-two"><span>帧间</span><i /></div>
        <div class="guest-art-corner corner-one" /><div class="guest-art-corner corner-two" />
      </div>
    </main>
    <footer class="guest-footer">帧间 · 从一个想法开始</footer>
  </div>
</template>

<style scoped>
.guest-home { width: 100%; height: 100%; overflow-y: auto; background: #111214; }
.guest-header { height: 88px; padding: 0 clamp(24px, 5vw, 76px); display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid #292c32; }
.guest-header .brand { color: inherit; text-decoration: none; }
.guest-sign-in { border: 1px solid #4a5361; border-radius: 8px; background: #20252d; color: #e5ecf9; padding: 10px 16px; font-size: 13px; cursor: pointer; }
.guest-sign-in:hover { background: #2a3340; }
.guest-main { width: min(1120px, calc(100% - 80px)); min-height: calc(100% - 150px); margin: 0 auto; display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, .85fr); align-items: center; gap: clamp(40px, 8vw, 130px); padding: 70px 0; }
.guest-copy { min-width: 0; }
.guest-eyebrow { margin: 0 0 22px; color: #8cacdf; font-size: 12px; letter-spacing: 3px; }
.guest-copy h1 { margin: 0; font-size: clamp(36px, 4.5vw, 62px); font-weight: 500; letter-spacing: 3px; line-height: 1.35; }
.guest-description { margin: 24px 0 32px; color: #adb1ba; font-size: 15px; line-height: 1.8; }
.guest-create { display: inline-flex; align-items: center; justify-content: space-between; gap: 34px; min-width: 170px; min-height: 48px; border: 1px solid #6c9fff; border-radius: 8px; background: #4f8eff; color: #081932; padding: 11px 18px; font-weight: 600; font-size: 14px; cursor: pointer; }
.guest-create:hover { background: #77a8ff; }
.guest-create span { font-size: 20px; line-height: 1; }
.guest-hint { margin: 15px 0 0; color: #9299a5; font-size: 12px; line-height: 1.7; }
.guest-notice, .guest-session-notice { max-width: 470px; margin: 28px 0 0; border-radius: 9px; padding: 14px 16px; font-size: 12px; line-height: 1.6; }
.guest-notice { border: 1px solid #685449; background: #2b2421; color: #f4c9bc; }
.guest-notice p { margin: 0 0 12px; }
.guest-notice button { border: 1px solid #8d6f5d; border-radius: 6px; background: #392c28; color: inherit; padding: 6px 10px; cursor: pointer; }
.guest-session-notice { border: 1px solid #4d5b71; background: #202630; color: #c8d8f2; }
.guest-art { height: min(480px, 48vw); min-height: 340px; position: relative; border: 1px solid #343b45; border-radius: 18px; background: #171a20; overflow: hidden; box-shadow: 0 24px 70px #0004; }
.guest-art-grid { position: absolute; inset: 0; background-image: radial-gradient(#495260 .7px, transparent .7px); background-size: 17px 17px; opacity: .5; }
.guest-art-frame { position: absolute; width: 58%; height: 40%; border: 1px solid #65758c; border-radius: 13px; background: #212936; box-shadow: 0 18px 38px #0006; }
.frame-one { top: 22%; left: 15%; transform: rotate(-9deg); }
.frame-two { top: 38%; left: 27%; transform: rotate(5deg); display: flex; align-items: center; justify-content: center; gap: 18px; border-color: #7e95b4; background: #2a3647; }
.frame-two span { color: #d3e1f7; font-size: 28px; letter-spacing: 7px; }
.frame-two i { width: 0; height: 0; border-top: 10px solid transparent; border-bottom: 10px solid transparent; border-left: 15px solid #8db5ff; }
.guest-art-corner { position: absolute; width: 24px; height: 24px; border-top: 1px solid #8795ac; border-left: 1px solid #8795ac; }
.corner-one { top: 12%; left: 9%; }
.corner-two { bottom: 10%; right: 9%; transform: rotate(180deg); }
.guest-footer { padding: 0 24px 25px; color: #868e9b; font-size: 11px; letter-spacing: 2px; text-align: center; }
@media (max-width: 740px) { .guest-header { height: 72px; padding: 0 22px; } .guest-main { width: calc(100% - 44px); display: block; min-height: calc(100% - 125px); padding: 72px 0 40px; } .guest-art { margin: 48px auto 0; height: 250px; min-height: 0; max-width: 480px; } .guest-copy h1 { font-size: clamp(35px, 9vw, 52px); } }
@media (max-width: 480px) { .guest-art { height: 205px; } .frame-two span { font-size: 20px; } .guest-sign-in { padding: 8px 12px; } }
</style>
