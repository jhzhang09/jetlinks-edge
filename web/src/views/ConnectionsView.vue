<script setup lang="ts">
import { computed, onMounted, ref, h } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NInput, NSelect, NSwitch, NSpace, useMessage, useDialog, NDataTable, NModal, NForm, NFormItem, NP } from 'naive-ui'
import {
  listDriverExtensions,
  listConnections,
  deleteConnection,
  createConnection,
  updateConnection,
  getConnection,
  configDefaults,
  type ExtensionDescriptor,
  type Connection
} from '@/api'
import { useI18n } from '@/i18n'
import { useOperations } from '@/composables/useOperations'
import DynamicConfigForm from '@/components/DynamicConfigForm.vue'

const { t } = useI18n()
const router = useRouter()
const msg = useMessage()
const dialog = useDialog()
const extensions = ref<ExtensionDescriptor[]>([])

// 接入 useOperations 共享状态
const { data, connectedConnections, enabledConnections, refresh: refreshOps } = useOperations(5000)

const connections = ref<Connection[]>([])

async function loadConnections() {
  try {
    const res = await listConnections()
    connections.value = res.items || []
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  }
}

// 新建
const showCreate = ref(false)
const formNew = ref<Partial<Connection>>({ driver: 'modbus-tcp', enabled: true, config: {} })
// 编辑
const showEdit = ref(false)
const editingId = ref('')
const formEdit = ref<Partial<Connection>>({})

async function refresh() {
  await loadConnections()
  await refreshOps()
}

const schemaFor = (driver?: string) => extensions.value.find(item => item.type === driver)?.connectionSchema || []
const driverOptions = () => extensions.value.map(item => ({ label: item.name, value: item.type }))

function changeDriver(form: Partial<Connection>, driver: string) {
  form.driver = driver
  form.config = configDefaults(schemaFor(driver))
}

function openCreate() {
  formNew.value = { driver: 'modbus-tcp', enabled: true, config: configDefaults(schemaFor('modbus-tcp')) }
  showCreate.value = true
}

async function loadDrivers() {
  extensions.value = (await listDriverExtensions()).items || []
  formNew.value.config = configDefaults(schemaFor(formNew.value.driver), formNew.value.config)
}

onMounted(() => {
  loadDrivers()
  loadConnections()
})

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

async function onCreate() {
  if (!formNew.value.name || !formNew.value.driver) {
    msg.warning(t('conn.required'))
    return
  }
  if (!validateConfig(formNew.value.config, schemaFor(formNew.value.driver))) return
  try {
    await createConnection(formNew.value)
    msg.success(t('conn.registered'))
    showCreate.value = false
    formNew.value = { driver: 'modbus-tcp', enabled: true, config: configDefaults(schemaFor('modbus-tcp')) }
    refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.create_failed'))
  }
}

async function openEdit(id: string) {
  try {
    const c = await getConnection(id)
    editingId.value = id
    showEdit.value = true
    formEdit.value = {
      name: c.name,
      description: c.description || '',
      driver: c.driver,
      enabled: c.enabled,
      config: c.config || {}
    }
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  }
}

async function onSaveEdit() {
  if (!formEdit.value.name || !formEdit.value.driver) {
    msg.warning(t('conn.required'))
    return
  }
  if (!validateConfig(formEdit.value.config, schemaFor(formEdit.value.driver))) return
  try {
    await updateConnection(editingId.value, formEdit.value)
    msg.success(t('conn.reloaded'))
    showEdit.value = false
    refresh()
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
        await deleteConnection(id)
        msg.success(t('conn.removed'))
        refresh()
      } catch (err: any) {
        msg.error(err.message || t('common.delete_failed'))
      }
    }
  })
}

const cols = [
  { title: 'ID', key: 'id', width: 180, render: (c: Connection) => h('code', { class: 'ops-mono', style: 'color:var(--amber);font-size:11px' }, c.id.slice(0, 12) + '…') },
  { title: t('conn.col_name'), key: 'name' },
  { title: t('conn.col_driver'), key: 'driver', width: 140, render: (c: Connection) => h('span', { class: 'ops-tag amber' }, c.driver) },
  {
    title: t('conn.col_status'),
    key: 'status',
    width: 100,
    render: (c: Connection) => {
      if (!c.enabled) return h('span', { class: 'ops-state dead' }, t('devices.status_off'))
      // 从 operations 全局缓存中提取该通道的实时连通性状态
      const connInfo = data.value.connections?.find(conn => conn.id === c.id)
      const connected = connInfo ? connInfo.connected : false
      return h('span', { class: 'ops-state ' + (connected ? 'live' : 'dead') }, t(connected ? 'devices.status_live' : 'devices.status_off'))
    }
  },
  { title: t('conn.desc'), key: 'description' },
  {
    title: t('conn.col_actions'),
    key: 'action',
    width: 220,
    render: (c: Connection) => h(NSpace, {}, () => [
      h(NButton, { class: 'ops-mini-button', size: 'small', onClick: () => openEdit(c.id) }, () => t('devices.action_edit')),
      h(NButton, { class: 'ops-mini-button', size: 'small', type: 'error', onClick: () => onDelete(c.id) }, () => t('devices.action_del'))
    ])
  }
]
</script>

<template>
  <div class="ops-page">
    <div class="ops-heading">
      <div>
        <h1 class="ops-title">{{ t('conn.title') }}</h1>
        <p class="ops-subtitle">{{ t('conn.subtitle') }}</p>
      </div>
      <div class="ops-actions">
        <button class="ops-button primary" @click="openCreate">{{ t('conn.register') }}</button>
        <button class="ops-button" @click="refresh">{{ t('devices.refresh') }}</button>
      </div>
    </div>
    
    <!-- 物理通道运行状态指标条 -->
    <section class="ops-panel health-panel">
      <div class="health-strip compact">
        <div><span>{{ t('conn.total_count') }}</span><strong>{{ data.connections?.length || 0 }}</strong><small>{{ t('conn.enabled') }} <b>{{ enabledConnections }}</b></small></div>
        <div><span>{{ t('conn.online_running') }}</span><strong>{{ connectedConnections }}</strong><small>{{ t('conn.offline') }} {{ Math.max((data.connections?.length || 0) - connectedConnections, 0) }}</small></div>
        <div><span>{{ t('conn.sub_groups') }}</span><strong>{{ data.groups?.length || 0 }}</strong><small>{{ t('conn.shared_desc') }}</small></div>
        <div><span>{{ t('conn.support_south') }}</span><strong>{{ data.driverPlugins?.length || 0 }}</strong><small>{{ t('conn.support_protocols') }}</small></div>
      </div>
    </section>

    <div class="ops-table-card">
      <n-data-table :columns="cols" :data="connections" :bordered="false" :pagination="false" size="small" />
    </div>

    <!-- 新建 -->
    <n-modal v-model:show="showCreate" class="ops-dialog" preset="dialog" :title="t('conn.register_title')" :positive-text="t('conn.register')" :negative-text="t('conn.cancel')" @positive-click="onCreate">
      <n-form :model="formNew" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('conn.name')" :required="true"><n-input v-model:value="formNew.name" /></n-form-item>
        <n-form-item :label="t('conn.driver')" :required="true"><n-select :value="formNew.driver" :options="driverOptions()" @update:value="changeDriver(formNew, $event)" /></n-form-item>
        <dynamic-config-form v-model="formNew.config!" :schema="schemaFor(formNew.driver)" />
        <n-p depth="3" class="ops-modal-note">{{ t('conn.tooltip') }}</n-p>
      </n-form>
    </n-modal>

    <!-- 编辑 -->
    <n-modal v-model:show="showEdit" class="ops-dialog" preset="dialog" :title="t('conn.edit_title')" :positive-text="t('conn.save')" :negative-text="t('conn.cancel')" @positive-click="onSaveEdit">
      <n-form v-if="editingId" :model="formEdit" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('conn.name')" :required="true"><n-input v-model:value="formEdit.name" /></n-form-item>
        <n-form-item :label="t('conn.desc')"><n-input v-model:value="formEdit.description" /></n-form-item>
        <n-form-item :label="t('conn.driver')" :required="true"><n-select :value="formEdit.driver" :options="driverOptions()" @update:value="changeDriver(formEdit, $event)" /></n-form-item>
        <n-form-item :label="t('gw.enabled')" v-if="false"><n-switch v-model:value="formEdit.enabled" /></n-form-item>
        <dynamic-config-form v-model="formEdit.config!" :schema="schemaFor(formEdit.driver)" />
      </n-form>
    </n-modal>
  </div>
</template>

<style scoped>
.health-panel { margin-bottom: 12px; }
.health-strip.compact { display: grid; grid-template-columns: repeat(4, 1fr); }
.health-strip.compact > div { min-height: 96px; padding: 16px; border-right: 1px solid var(--line); }
.health-strip.compact > div:last-child { border-right: 0; }
.health-strip span, .health-strip small { display: block; color: var(--muted); font-size: 13px; }
.health-strip strong { display: block; margin: 9px 0 8px; color: #eef4f6; font: 650 28px/1 var(--mono); }
.health-strip small b { color: var(--cyan); }
@media (max-width: 900px) { .health-strip.compact { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 620px) { .health-strip.compact { grid-template-columns: 1fr; } }
.ops-modal-note { margin-top: 12px; font-size: 12px; color: var(--muted); }
</style>
