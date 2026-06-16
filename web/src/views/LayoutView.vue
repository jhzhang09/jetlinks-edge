<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import { useOperations } from '@/composables/useOperations'
import { useThemeStore } from '@/stores/theme'
import ChangePasswordModal from '@/components/ChangePasswordModal.vue'

const themeStore = useThemeStore()
const { t, lang, toggleLang } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const { data, criticalAlarms, enabledGroups } = useOperations(10000)
const now = ref(new Date())
const collapsed = ref(false) // 菜单栏折叠状态
let clock: number | undefined

onMounted(() => {
  if (auth.isLoggedIn) auth.loadProfile()
  clock = window.setInterval(() => { now.value = new Date() }, 1000)
})
onUnmounted(() => clock && window.clearInterval(clock))

const menuItems = computed(() => [
  { label: t('nav.overview'), code: 'OV', path: '/dashboard' },
  { label: t('nav.connections'), code: 'CN', path: '/connections' },
  { label: t('nav.devices'), code: 'SB', path: '/groups' },
  { label: t('nav.topology'), code: 'TP', path: '/topology' },
  { label: t('nav.gateway'), code: 'NB', path: '/northbound' },
  { label: t('nav.alarms'), code: 'AL', path: '/alarms', count: data.value.alarms.length }
])
const uptime = computed(() => {
  if (!data.value.startTime) return '--'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(data.value.startTime).getTime()) / 1000))
  return `${Math.floor(seconds / 86400)}天 ${String(Math.floor(seconds % 86400 / 3600)).padStart(2, '0')}:${String(Math.floor(seconds % 3600 / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
})
const memoryText = computed(() => {
  const alloc = data.value.runtime.memoryAllocBytes
  if (!alloc) return '--'
  return `${(alloc / 1024 / 1024).toFixed(1)}MB`
})
function active(path: string) { return path === '/groups' || path === '/connections' ? route.path.startsWith(path) : route.path === path }
function logout() { auth.logout(); router.push('/login') }

// --- 用户管理：修改密码功能 ---
const showPasswordModal = ref(false)

const dropdownOptions = computed(() => [
  { label: t('top.change_password'), key: 'password' },
  { label: t('top.logout'), key: 'logout' }
])

function handleUserAction(key: string) {
  if (key === 'logout') {
    logout()
  } else if (key === 'password') {
    showPasswordModal.value = true
  }
}
</script>

<template>
  <div class="shell">
    <aside class="sidebar" :class="{ collapsed }">
      <button class="brand" type="button" @click="router.push('/dashboard')"><span class="brand-mark">JE</span><strong>JetLinks Edge</strong></button>
      <nav class="nav">
        <button v-for="item in menuItems" :key="item.path" type="button" :class="{ active: active(item.path) }" @click="router.push(item.path)">
          <span class="nav-code">{{ item.code }}</span><span>{{ item.label }}</span><b v-if="item.count">{{ item.count }}</b>
        </button>
      </nav>
      <button class="collapse" type="button" @click="collapsed = !collapsed">
        {{ collapsed ? '›' : t('top.collapse') }}
      </button>
    </aside>

    <main class="workspace" :class="{ collapsed }">
      <header class="topbar">
        <div class="gateway"><span>{{ t('top.gateway') }}</span><strong :title="data.runtime.nodeId">{{ data.runtime.nodeId }}</strong><i></i><em>{{ t('top.running') }}</em></div>
        <div class="runtime"><span>{{ t('top.uptime') }}<strong>{{ uptime }}</strong></span><span>{{ t('top.localtime') }}<strong>{{ now.toLocaleString() }}</strong></span></div>
        <div class="resources"><span>{{ t('top.goroutines') }}<strong>{{ data.runtime.goroutines }}</strong></span><span>{{ t('top.memory') }}<strong>{{ memoryText }}</strong></span><span>{{ t('top.tasks') }}<strong>{{ enabledGroups }}</strong></span></div>
        <div class="account">
          <button type="button" @click="router.push('/alarms')">{{ t('top.alarms') }} <b>{{ data.alarms.length }}</b></button>
          <button type="button" @click="themeStore.toggleTheme" class="theme-toggle-btn" :title="themeStore.theme === 'dark' ? t('top.theme_light') : t('top.theme_dark')">
            {{ themeStore.theme === 'dark' ? '☀️' : '🌙' }}
          </button>
          <button type="button" @click="toggleLang">{{ lang === 'zh' ? 'EN' : '中文' }}</button>
          <n-dropdown trigger="click" :options="dropdownOptions" @select="handleUserAction">
            <button type="button">{{ auth.user?.username || 'admin' }} <span style="font-size: 8px; opacity: 0.7; margin-left: 2px;">▼</span></button>
          </n-dropdown>
        </div>
      </header>
      <div class="content"><RouterView /></div>
    </main>

    <!-- 修改密码弹窗 -->
    <ChangePasswordModal v-model:show="showPasswordModal" @success="logout" />
  </div>
</template>

<style scoped>
.shell { min-height: 100vh; display: flex; background: var(--bg); }.sidebar { position: fixed; inset: 0 auto 0 0; z-index: 10; width: 204px; display: flex; flex-direction: column; border-right: 1px solid var(--line); background: #0b151e; transition: width 0.2s ease-in-out; }
.brand { height: 66px; display: flex; align-items: center; gap: 11px; padding: 0 18px; border: 0; border-bottom: 1px solid var(--line); background: transparent; color: #edf4f6; cursor: pointer; }.brand-mark { width: 32px; height: 32px; display: grid; place-items: center; border: 1px solid var(--cyan); color: var(--cyan); font: 700 11px var(--mono); }.brand strong { font-size: 17px; }
.nav { flex: 1; padding: 9px 0; }.nav button { width: 100%; height: 54px; display: grid; grid-template-columns: 30px 1fr 24px; align-items: center; padding: 0 18px; border: 0; border-left: 3px solid transparent; background: transparent; color: #a5b5bd; font-size: 15px; font-weight: 600; text-align: left; cursor: pointer; }.nav button:hover, .nav button.active { background: #12232d; color: #e7f0f3; }.nav button.active { border-left-color: var(--cyan); color: var(--cyan); }.nav-code { color: #58717e; font: 11px var(--mono); }.nav b, .account b { display: grid; place-items: center; min-width: 20px; height: 20px; border-radius: 10px; background: var(--red); color: white; font-size: 12px; }
.collapse { height: 50px; border: 0; border-top: 1px solid var(--line); background: transparent; color: var(--muted); text-align: left; padding-left: 22px; cursor: pointer; transition: padding 0.2s ease-in-out; }.workspace { width: calc(100% - 204px); min-height: 100vh; margin-left: 204px; transition: width 0.2s ease-in-out, margin-left 0.2s ease-in-out; }
.topbar { height: 66px; position: sticky; top: 0; z-index: 8; display: grid; grid-template-columns: 330px 340px 1fr auto; align-items: center; border-bottom: 1px solid var(--line); background: rgba(7,16,25,.98); }.gateway, .runtime, .resources, .account { min-width: 0; height: 100%; display: flex; align-items: center; border-right: 1px solid var(--line); }
.gateway { gap: 9px; padding: 0 16px; }.gateway span, .runtime span, .resources span { color: var(--muted); font-size: 12px; }.gateway strong { min-width: 132px; max-width: 158px; overflow: hidden; padding: 9px 12px; border: 1px solid var(--line-strong); color: #dce7eb; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }.gateway i { width: 7px; height: 7px; border-radius: 50%; background: var(--green); }.gateway em { color: var(--green); font-size: 12px; font-style: normal; }
.runtime span { min-width: 160px; padding: 0 14px; }.runtime strong { display: block; margin-top: 5px; color: #d2dde1; font: 12px var(--mono); }.resources { justify-content: center; gap: 24px; }.resources span { min-width: 64px; }.resources b { display: block; width: 55px; height: 4px; margin-top: 7px; background: var(--cyan); box-shadow: inset -34px 0 #253640; }
.resources strong { display: block; margin-top: 5px; color: var(--cyan); font: 12px var(--mono); }
.account { padding: 0 10px; border-right: 0; }.account button { height: 36px; padding: 0 10px; display: flex; align-items: center; gap: 6px; border: 0; background: transparent; color: #b3c1c8; font-size: 12px; cursor: pointer; }
.theme-toggle-btn { font-size: 15px !important; transition: transform 0.2s ease; }
.theme-toggle-btn:hover { transform: scale(1.18) rotate(12deg); }
.content { padding: 18px 20px 28px; }

/* 菜单栏收起后的响应式布局 */
.sidebar.collapsed { width: 68px; }
.sidebar.collapsed .brand strong { display: none; }
.sidebar.collapsed .nav button { grid-template-columns: 1fr; padding: 0; text-align: center; }
.sidebar.collapsed .nav button span:nth-child(2) { display: none; }
.sidebar.collapsed .nav-code { text-align: center; }
.sidebar.collapsed .collapse { text-align: center; padding-left: 0; }
.workspace.collapsed { width: calc(100% - 68px); margin-left: 68px; }

@media (max-width: 1050px) { .topbar { grid-template-columns: 1fr auto; }.runtime, .resources { display: none; } }
@media (max-width: 760px) { .sidebar { width: 68px; }.brand { padding: 0 18px; }.brand strong, .nav button span:nth-child(2), .collapse { display: none; }.nav button { grid-template-columns: 1fr; padding: 0; text-align: center; }.nav-code { text-align: center; }.workspace { width: calc(100% - 68px); margin-left: 68px; }.topbar { height: 50px; grid-template-columns: 1fr; }.gateway { border: 0; }.gateway span, .gateway strong { display: none; }.account { display: none; }.content { padding: 12px; } }
</style>
