<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'

const { t, lang, toggleLang, setLang } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const username = ref('admin')
const password = ref('admin123')
const loading = ref(false)
const failed = ref('')

async function onSubmit() {
  loading.value = true
  failed.value = ''
  try {
    await auth.login(username.value, password.value)
    const redirect = (route.query.redirect as string) || '/dashboard'
    router.push(redirect)
  } catch (e: any) {
    failed.value = e?.response?.data?.error || t('login.auth_failed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="shell">
    <div class="noise"></div>
    <div class="scanline"></div>
    <div class="login-panel">
      <div class="lang-switch">
        <button :class="{ active: lang === 'zh' }" @click="toggleLang">中</button>
        <span>/</span>
        <button :class="{ active: lang === 'en' }" @click="setLang('en')">EN</button>
      </div>
      <div class="brand">
        <div class="brand-sigil">
          <svg viewBox="0 0 48 48" fill="none"><rect x="4" y="16" width="40" height="20" rx="1" stroke="currentColor" stroke-width="2"/><rect x="10" y="20" width="28" height="12" rx="0.5" stroke="currentColor" stroke-width="1.5"/><circle cx="18" cy="26" r="2" fill="#e8a838"/><circle cx="26" cy="26" r="1.5" fill="#38b2e8"/><circle cx="34" cy="13" r="3" stroke="#38b2e8" stroke-width="1.2"/><path d="M34 13l-2-2M34 13l2-2M34 13l-2 2M34 13l2 2" stroke="#38b2e8" stroke-width="0.8"/></svg>
        </div>
        <h1>{{ t('login.title') }}</h1>
        <p class="tagline">{{ t('login.subtitle') }}</p>
      </div>
      <form class="form" @submit.prevent="onSubmit">
        <div class="field">
          <label class="field-label" for="user">{{ t('login.user_id') }}</label>
          <input id="user" v-model="username" type="text" autocomplete="off" placeholder="admin" />
        </div>
        <div class="field">
          <label class="field-label" for="pass">{{ t('login.secure_key') }}</label>
          <input id="pass" v-model="password" type="password" placeholder="········" />
        </div>
        <div v-if="failed" class="error">{{ failed }}</div>
        <button type="submit" :disabled="loading" :class="{ busy: loading }">
          <span class="btn-inner">{{ loading ? t('login.auth_pending') : t('login.connect') }}</span>
        </button>
      </form>
      <div class="footer-info">
        <span>{{ t('login.node') }}</span>
        <span>{{ t('login.proto') }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.shell {
  position: relative; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  background: radial-gradient(ellipse at 30% 20%, rgba(56,178,232,0.06) 0%, transparent 60%),
              radial-gradient(ellipse at 70% 80%, rgba(232,168,56,0.04) 0%, transparent 50%),
              #080C11;
}
.noise { position: fixed; inset: 0; opacity: 0.015; pointer-events: none;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}
.scanline { position: fixed; inset: 0; pointer-events: none; z-index: 999;
  background: repeating-linear-gradient(0deg, transparent, transparent 2px, rgba(0,0,0,0.03) 2px, rgba(0,0,0,0.03) 4px);
}
.login-panel {
  position: relative; z-index: 10; width: 420px; padding: 48px 40px 36px;
  background: linear-gradient(180deg, rgba(16,22,29,0.98) 0%, rgba(8,12,17,0.95) 100%);
  border: 1px solid #1a2430; border-radius: 2px;
  box-shadow: 0 0 0 1px rgba(56,178,232,0.06), 0 24px 60px rgba(0,0,0,0.6);
}
.brand { text-align: center; margin-bottom: 40px; }
.lang-switch { display: flex; justify-content: flex-end; align-items: center; gap: 4px; margin-bottom: 12px; font-size: 12px; }
.lang-switch button { padding: 2px 8px; font: 12px var(--mono); color: #4a5b68; background: transparent; border: 1px solid #1c2733; border-radius: 2px; cursor: pointer; }
.lang-switch button.active { color: #e8a838; border-color: #e8a838; }
.lang-switch span { color: #2a3542; }
.brand-sigil { width: 48px; height: 48px; margin: 0 auto 16px; color: #38b2e8; }
.brand-sigil svg { width: 100%; height: 100%; }
h1 { margin: 0; color: #e4e8eb; font-size: 24px; font-weight: 650; white-space: pre; }
.tagline { margin: 6px 0 0; font-size: 12px; color: #4a5b68; }
.field { margin-bottom: 20px; }
.field-label { display: block; font-size: 12px; color: #5a6b78; margin-bottom: 6px; }
input {
  width: 100%; padding: 12px 14px; color: #e4e8eb; font-size: 14px;
  background: #0d141c; border: 1px solid #1c2733; border-radius: 2px; outline: none;
  transition: border-color 0.2s, box-shadow 0.2s;
}
input:focus { border-color: #38b2e8; box-shadow: 0 0 0 3px rgba(56,178,232,0.08); }
input::placeholder { color: #2a3542; }
.error { padding: 8px 12px; margin-bottom: 16px; font-size: 12px; color: #e8384f; background: rgba(232,56,79,0.1);
  border: 1px solid rgba(232,56,79,0.2); border-radius: 2px; text-align: center; }
button {
  width: 100%; padding: 14px; margin-top: 4px; color: #0d141c; font-size: 13px; font-weight: 650;
  background: linear-gradient(135deg, #e8a838, #d48c20); border: none; border-radius: 2px; cursor: pointer;
  transition: filter 0.2s, transform 0.15s;
}
button:hover:not(:disabled) { filter: brightness(1.15); transform: translateY(-1px); }
button:active:not(:disabled) { transform: translateY(0); }
button:disabled { opacity: 0.5; cursor: not-allowed; }
.busy .btn-inner::after { content: ''; display: inline-block; width: 8px; height: 8px; margin-left: 8px;
  border: 2px solid transparent; border-top-color: #0d141c; border-radius: 50%; animation: spin 0.6s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.footer-info { margin-top: 32px; display: flex; justify-content: space-between;
  font-size: 12px; color: #3a4b58; }
</style>

<style>
/* 
 * 浅色模式（白天模式）下的登录页面全局样式覆盖 
 * 由于使用非 scoped 样式，可 100% 保证样式原样编译输出，不受哈希属性干扰，完美覆盖暗色样式
 */
html[data-theme='light'] .shell {
  background: radial-gradient(ellipse at 30% 20%, rgba(14,165,233,0.04) 0%, transparent 60%),
              radial-gradient(ellipse at 70% 80%, rgba(217,119,6,0.03) 0%, transparent 50%),
              #f4f6f8 !important;
}

html[data-theme='light'] .login-panel {
  background: #ffffff !important;
  background-image: none !important;
  border: 1px solid #e2e8f0 !important;
  box-shadow: 0 0 0 1px rgba(14,165,233,0.04), 0 20px 50px rgba(8, 20, 32, 0.05) !important;
}

html[data-theme='light'] .lang-switch button {
  color: #64748b !important;
  border-color: #e2e8f0 !important;
}

html[data-theme='light'] .lang-switch button.active {
  color: #d97706 !important;
  border-color: #d97706 !important;
}

html[data-theme='light'] .lang-switch span {
  color: #cbd5e1 !important;
}

html[data-theme='light'] .brand-sigil {
  color: #0ea5e9 !important;
}

html[data-theme='light'] h1 {
  color: #1e293b !important;
}

html[data-theme='light'] .tagline {
  color: #64748b !important;
}

html[data-theme='light'] .field-label {
  color: #64748b !important;
}

html[data-theme='light'] input {
  color: #1e293b !important;
  background: #f8fafc !important;
  border-color: #cbd5e1 !important;
}

html[data-theme='light'] input:focus {
  background: #ffffff !important;
  border-color: #0ea5e9 !important;
  box-shadow: 0 0 0 3px rgba(14,165,233,0.1) !important;
}

html[data-theme='light'] input::placeholder {
  color: #94a3b8 !important;
}

html[data-theme='light'] .footer-info {
  color: #64748b !important;
}
</style>
