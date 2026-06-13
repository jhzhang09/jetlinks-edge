<script setup lang="ts">
import { computed, onMounted, ref, h } from 'vue'
import { NSpace, useMessage, useDialog, NButton, NInput, NSwitch, NSelect, NTooltip } from 'naive-ui'
import { listNorthApps, listNorthExtensions, createNorthApp, updateNorthApp, deleteNorthApp, reloadNorthApp, configDefaults, type ExtensionDescriptor, type NorthApp, type NorthAppStatus } from '@/api'
import { useI18n } from '@/i18n'
import DynamicConfigForm from '@/components/DynamicConfigForm.vue'

const { t } = useI18n()
const msg = useMessage()
const dialog = useDialog()
const apps = ref<NorthAppStatus[]>([])
const types = ref<string[]>([])
const extensions = ref<ExtensionDescriptor[]>([])
const loading = ref(false)

const showForm = ref(false); const editing = ref<Partial<NorthApp> | null>(null)
const form = ref<Partial<NorthApp>>({ name:'', type:'jetlinks-mqtt', enabled:true,
  config: {} })

const schemaFor = (type?: string) => extensions.value.find(item => item.type === type)?.configSchema || []
const extensionOptions = () => extensions.value.map(item => ({ label: t(item.name), value: item.type }))
const descriptorFor = (type?: string) => extensions.value.find(item => item.type === type)
const enabledCount = computed(() => apps.value.filter(item => item.enabled).length)
const connectedCount = computed(() => apps.value.filter(item => item.connected).length)
const runningCount = computed(() => apps.value.filter(item => item.running).length)
function changeType(type: string) {
  form.value.type = type
  form.value.config = configDefaults(schemaFor(type))
}

async function refresh() {
  loading.value = true
  try {
    const [appsResult, extensionResult] = await Promise.all([listNorthApps(), listNorthExtensions()])
    apps.value = appsResult.items || []
    types.value = appsResult.types || []
    extensions.value = extensionResult.items || []
  } finally { loading.value = false }
}
onMounted(refresh)

function openCreate() {
  editing.value = null
  form.value = { name:'', type:'jetlinks-mqtt', enabled:true,
    config: configDefaults(schemaFor('jetlinks-mqtt')) }
  showForm.value = true
}
async function openEdit(id: string) {
  try {
    const r = await import('@/api'); const detail = await r.getNorthApp(id)
    editing.value = detail
    form.value = { name: detail.name, description: detail.description, type: detail.type, enabled: detail.enabled, config: detail.config || {} }
    showForm.value = true
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  }
}
function validateConfig(config: Record<string, any> | undefined, schema: any[]): boolean {
  const cfg = config || {}
  for (const field of schema) {
    if (field.required) {
      const val = cfg[field.key]
      if (val === undefined || val === null || (typeof val === 'string' && val.trim() === '') || (typeof val === 'number' && isNaN(val))) {
        msg.warning(`${field.label} ${t('gw.required_field')}`)
        return false
      }
    }
  }
  return true
}

async function onSubmit() {
  if (!form.value.name || !form.value.type) { msg.warning(t('gw.required')); return }
  if (!validateConfig(form.value.config, schemaFor(form.value.type))) return
  try {
    if (editing.value?.id) { await updateNorthApp(editing.value.id, form.value); msg.success(t('gw.updated')) }
    else { await createNorthApp(form.value); msg.success(t('gw.registered')) }
    showForm.value = false; refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.save_failed'))
  }
}
function onDelete(id: string) {
  dialog.warning({
    title: t('gw.action_del'),
    content: t('gw.delete_confirm'),
    positiveText: 'OK',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await deleteNorthApp(id)
        msg.success(t('gw.removed'))
        refresh()
      } catch (err: any) {
        msg.error(err.message || t('common.delete_failed'))
      }
    }
  })
}
async function onReload(id: string) {
  try {
    await reloadNorthApp(id); msg.success(t('gw.reloaded')); refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.reload_failed'))
  }
}

const cols = [
  { title: t('gw.col_id'), key: 'id', width: 180, render: (a: NorthAppStatus) => h('code', { class:'ops-mono', style: 'color:var(--amber);font-size:11px' }, a.id.slice(0,12)+'…') },
  { title: t('gw.col_name'), key: 'name' },
  { title: t('gw.col_type'), key: 'type', width: 180, render: (a: NorthAppStatus) => h('span', { class: 'ops-tag cyan' }, t(descriptorFor(a.type)?.name || a.type)) },
  {
    title: t('gw.col_state'), key: 'running', width: 100,
    render: (a: NorthAppStatus) => {
      if (!a.enabled) return h('span', { class: 'ops-state dead' }, t('gw.state_off'))
      if (a.connected) return h('span', { class: 'ops-state live' }, t('gw.state_online'))
      if (a.running && a.lastError) {
        return h(NTooltip, { trigger: 'hover', placement: 'top' }, {
          trigger: () => h('span', { class: 'ops-state err', style: 'cursor: pointer;' }, t('gw.state_err')),
          default: () => a.lastError
        })
      }
      if (a.running) return h('span', { class: 'ops-state warn' }, t('gw.state_starting'))
      return h('span', { class: 'ops-state dead' }, t('gw.state_off'))
    }
  },
  { title: t('gw.col_enabled'), key: 'enabled', width: 80, render: (a: NorthAppStatus) => h('span', { class: 'ops-tag '+(a.enabled?'cyan':'dim') }, t(a.enabled?'gw.enabled_yes':'gw.enabled_no')) },
  { title: t('gw.col_actions'), key: 'action', width: 260,
    render: (a: NorthAppStatus) => h(NSpace, {}, () => [
      h(NButton, { class:'ops-mini-button', size: 'small', onClick: () => openEdit(a.id) }, () => t('gw.action_edit')),
      h(NButton, { class:'ops-mini-button', size: 'small', type: 'warning', onClick: () => onReload(a.id) }, () => t('gw.action_reload')),
      h(NButton, { class:'ops-mini-button', size: 'small', type: 'error', onClick: () => onDelete(a.id) }, () => t('gw.action_del'))
    ])
  }
]
</script>

<template>
  <div class="ops-page">
    <div class="ops-heading">
      <div>
        <h1 class="ops-title">{{ t('gw.title') }}</h1>
        <p class="ops-subtitle">{{ t('gw.subtitle') }}</p>
      </div>
      <div class="ops-actions">
        <button class="ops-button primary" @click="openCreate">{{ t('gw.register') }}</button>
        <button class="ops-button" @click="refresh">{{ t('gw.refresh') }}</button>
      </div>
    </div>

    <section class="ops-panel health-panel">
      <div class="health-strip compact">
        <div><span>{{ t('north.total_count') }}</span><strong>{{ apps.length }}</strong><small>{{ t('north.enabled') }} <b>{{ enabledCount }}</b></small></div>
        <div><span>{{ t('north.connected') }}</span><strong>{{ connectedCount }}</strong><small>{{ t('north.running') }} {{ runningCount }}</small></div>
        <div><span>{{ t('north.plugins') }}</span><strong>{{ extensions.length }}</strong><small>{{ t('north.schema_driven') }}</small></div>
        <div><span>{{ t('north.channel_types') }}</span><strong>{{ types.length }}</strong><small>{{ t('north.compile_registered') }}</small></div>
      </div>
    </section>

    <div class="ops-table-card">
      <n-data-table :columns="cols" :data="apps" :bordered="false" :pagination="false" size="small" />
    </div>

    <n-modal v-model:show="showForm" class="ops-dialog" preset="dialog" :title="t(editing?'gw.edit_title':'gw.register_title')" :positive-text="t('gw.save')" :negative-text="t('gw.cancel')" @positive-click="onSubmit">
      <n-form :model="form" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('gw.name')" :required="true"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item :label="t('gw.desc')"><n-input v-model:value="form.description" /></n-form-item>
        <n-form-item :label="t('gw.type')" :required="true"><n-select :value="form.type" :options="extensionOptions()" @update:value="changeType" /></n-form-item>
        <n-form-item :label="t('gw.enabled')"><n-switch v-model:value="form.enabled" /></n-form-item>
        <n-divider>PLUGIN CONFIG</n-divider>
        <!-- 动态显示插件自身描述 -->
        <n-p depth="3" v-if="descriptorFor(form.type)?.description" style="margin-bottom:12px; font-style:italic">
          {{ t(descriptorFor(form.type)?.description || '') }}
        </n-p>

        <!-- 选择 jetlinks-mqtt 网关插件时，动态提示其特有的网络架构与认证方式 -->
        <div v-if="form.type === 'jetlinks-mqtt'" class="ops-info">
          <h3>{{ t('gw.alert_title') }}</h3>
          <p>{{ t('gw.alert_text') }}</p>
        </div>

        <dynamic-config-form v-model="form.config!" :schema="schemaFor(form.type)" />
        
        <!-- 仅针对 jetlinks-mqtt 网关动态展示连接与子设备凭证提示 -->
        <n-p depth="3" v-if="form.type === 'jetlinks-mqtt'" class="ops-modal-note">{{ t('gw.tooltip') }}</n-p>
      </n-form>
    </n-modal>
  </div>
</template>

<style scoped>
.health-panel { margin-bottom: 12px; }.health-strip.compact { display: grid; grid-template-columns: repeat(4, 1fr); }.health-strip.compact > div { min-height: 96px; padding: 16px; border-right: 1px solid var(--line); }.health-strip.compact > div:last-child { border-right: 0; }.health-strip span, .health-strip small { display: block; color: var(--muted); font-size: 13px; }.health-strip strong { display: block; margin: 9px 0 8px; color: #eef4f6; font: 650 28px/1 var(--mono); }.health-strip small b { color: var(--cyan); }
@media (max-width: 900px) { .health-strip.compact { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 620px) { .health-strip.compact { grid-template-columns: 1fr; }.ops-actions { flex-wrap: wrap; } }
</style>
