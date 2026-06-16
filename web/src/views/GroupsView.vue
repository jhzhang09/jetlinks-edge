<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, h } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NInput, NInputNumber, NSelect, NSwitch, NSpace, useMessage, useDialog, NDataTable, NModal, NForm, NFormItem, NP, NCheckbox } from 'naive-ui'
import {
  listGroups,
  listDriverExtensions,
  deleteGroup,
  reloadGroup,
  createGroup,
  updateGroup,
  getGroup,
  listNorthApps,
  listConnections,
  configDefaults,
  type ExtensionDescriptor,
  type Group,
  type Connection,
  type NorthAppStatus
} from '@/api'
import { useI18n } from '@/i18n'
import DynamicConfigForm from '@/components/DynamicConfigForm.vue'

const { t } = useI18n()
const router = useRouter()
const msg = useMessage()
const dialog = useDialog()
const groups = ref<Group[]>([])
const connections = ref<Connection[]>([])
const extensions = ref<ExtensionDescriptor[]>([])
const northApps = ref<NorthAppStatus[]>([])
const statusMap = ref<Record<string, boolean>>({})

const isJetLinksGateway = (appIds?: string[] | string) => {
  if (!appIds) return false
  const ids = Array.isArray(appIds) ? appIds : (typeof appIds === 'string' ? appIds.split(',') : [])
  return ids.some(id => northApps.value.find(a => a.id === id)?.type === 'jetlinks-mqtt')
}

const isAppSelected = (form: any, appId: string) => {
  return form.northAppIds && form.northAppIds.includes(appId)
}

const toggleAppSelection = (form: any, appId: string) => {
  if (!form.northAppIds) {
    form.northAppIds = []
  }
  const idx = form.northAppIds.indexOf(appId)
  if (idx > -1) {
    form.northAppIds.splice(idx, 1)
  } else {
    form.northAppIds.push(appId)
  }
}

// 新建
const showCreate = ref(false)
const formNew = ref<any>({ connectionId: '', intervalMs: 1000, enabled: true, config: {}, device: { productId: '', deviceId: '' }, northAppIds: [] })
// 编辑
const showEdit = ref(false)
const editingId = ref('')
const formEdit = ref<any>({})

async function refresh() {
  const r = await listGroups()
  groups.value = (r.items || []) as Group[]
}

async function loadConnections() {
  const r = await listConnections()
  connections.value = (r.items || []) as Connection[]
}

const schemaFor = (driver?: string) => extensions.value.find(item => item.type === driver)?.configSchema || []
const connectionOptions = computed(() => connections.value.map(c => ({ label: `${c.name} (${c.driver})`, value: c.id })))
const northAppOptions = computed(() => northApps.value.filter(a => a.running).map(a => ({ label: `${a.name} (${a.type})`, value: a.id })))

function onConnectionChange(form: Partial<Group>, connId: string) {
  form.connectionId = connId
  const conn = connections.value.find(c => c.id === connId)
  if (conn) {
    form.driver = conn.driver
    form.config = configDefaults(schemaFor(conn.driver))
  } else {
    form.driver = undefined
    form.config = {}
  }
}

function openCreate() {
  formNew.value = {
    connectionId: '',
    intervalMs: 1000,
    enabled: true,
    config: {},
    device: { productId: '', deviceId: '' }
  }
  showCreate.value = true
}

async function loadDrivers() {
  extensions.value = (await listDriverExtensions()).items || []
}

async function loadNorthApps() {
  northApps.value = (await listNorthApps()).items || []
}

async function loadStatus() {
  try {
    const { status: stFn } = await import('@/api')
    const s = await stFn()
    // 逻辑采集组状态，直接以 groupId 作为 key
    for (const [gid, drv] of Object.entries(s.drivers || {})) {
      statusMap.value[gid] = (drv as any)?.connected || false
    }
  } catch {}
}

let statusInterval: number | undefined
onMounted(() => {
  refresh()
  loadConnections()
  loadDrivers()
  loadNorthApps()
  loadStatus()
  statusInterval = window.setInterval(loadStatus, 5000)
})

onUnmounted(() => {
  if (statusInterval) {
    clearInterval(statusInterval)
  }
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
  if (!formNew.value.name || !formNew.value.connectionId) {
    msg.warning(t('devices.required'))
    return
  }
  if (formNew.value.intervalMs === undefined || formNew.value.intervalMs === null || formNew.value.intervalMs < 100) {
    msg.warning(t('devices.interval') + ' ' + t('gw.required_field'))
    return
  }
  if (isJetLinksGateway(formNew.value.northAppIds)) {
    if (!formNew.value.device?.productId || !formNew.value.device?.deviceId) {
      msg.warning(t('devices.product_id') + ' / ' + t('devices.device_id') + ' ' + t('gw.required_field'))
      return
    }
  }
  if (!validateConfig(formNew.value.config, schemaFor(formNew.value.driver))) return
  try {
    const payload = { ...formNew.value }
    payload.northAppId = payload.northAppIds ? payload.northAppIds.filter(Boolean).join(',') : ''
    delete payload.northAppIds

    await createGroup(payload)
    msg.success(t('devices.registered'))
    showCreate.value = false
    formNew.value = { connectionId: '', intervalMs: 1000, enabled: true, config: {}, device: { productId: '', deviceId: '' }, northAppIds: [] }
    refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.create_failed'))
  }
}

async function openEdit(id: string) {
  try {
    const g = await getGroup(id)
    editingId.value = id
    showEdit.value = true
    formEdit.value = {
      name: g.name,
      description: g.description || '',
      connectionId: g.connectionId,
      driver: g.driver,
      intervalMs: g.intervalMs,
      enabled: g.enabled,
      northAppIds: g.northAppId ? g.northAppId.split(',').filter(Boolean) : [],
      config: g.config || {},
      device: g.device || { productId: '', deviceId: '' }
    }
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  }
}

async function onSaveEdit() {
  if (!formEdit.value.name || !formEdit.value.connectionId) {
    msg.warning(t('devices.required'))
    return
  }
  if (formEdit.value.intervalMs === undefined || formEdit.value.intervalMs === null || formEdit.value.intervalMs < 100) {
    msg.warning(t('devices.interval') + ' ' + t('gw.required_field'))
    return
  }
  if (isJetLinksGateway(formEdit.value.northAppIds)) {
    if (!formEdit.value.device?.productId || !formEdit.value.device?.deviceId) {
      msg.warning(t('devices.product_id') + ' / ' + t('devices.device_id') + ' ' + t('gw.required_field'))
      return
    }
  }
  if (!validateConfig(formEdit.value.config, schemaFor(formEdit.value.driver))) return
  try {
    const payload = { ...formEdit.value }
    payload.northAppId = payload.northAppIds ? payload.northAppIds.filter(Boolean).join(',') : ''
    delete payload.northAppIds

    await updateGroup(editingId.value, payload)
    msg.success(t('devices.reloaded'))
    showEdit.value = false
    refresh()
    loadStatus()
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
        await deleteGroup(id)
        msg.success(t('devices.removed'))
        refresh()
      } catch (err: any) {
        msg.error(err.message || t('common.delete_failed'))
      }
    }
  })
}

async function onReload(id: string) {
  try {
    await reloadGroup(id)
    msg.success(t('devices.reloaded'))
    refresh()
    loadStatus()
  } catch (err: any) {
    msg.error(err.message || t('common.reload_failed'))
  }
}

const naLabel = (id?: string) => id ? (northApps.value.find(x => x.id === id)?.name || id.slice(0, 8)) : t('devices.gateway_none')
const connLabel = (id: string) => connections.value.find(x => x.id === id)?.name || id.slice(0, 8)
const enabledCount = computed(() => groups.value.filter(item => item.enabled).length)
const connectedCount = computed(() => groups.value.filter(item => statusMap.value[item.id] === true).length)
const linkedCount = computed(() => groups.value.filter(item => item.northAppId).length)

const cols = [
  { title: 'ID', key: 'id', width: 180, render: (g: Group) => h('code', { class: 'ops-mono', style: 'color:var(--amber);font-size:11px' }, g.id.slice(0, 12) + '…') },
  { title: t('devices.col_name'), key: 'name' },
  { title: t('devices.connection'), key: 'connectionId', width: 160, render: (g: Group) => h('span', { class: 'ops-tag cyan' }, connLabel(g.connectionId)) },
  { title: t('devices.col_driver'), key: 'driver', width: 120, render: (g: Group) => h('span', { class: 'ops-tag amber' }, g.driver || '--') },
  { title: t('devices.col_interval'), key: 'intervalMs', width: 90, render: (g: Group) => h('code', { class: 'ops-mono', style: 'color:var(--cyan)' }, g.intervalMs + 'ms') },
  {
    title: t('devices.gateway'),
    key: 'northAppId',
    width: 180,
    render: (g: Group) => {
      if (!g.northAppId) {
        return h('span', { class: 'ops-tag dim' }, t('devices.gateway_none'))
      }
      const ids = g.northAppId.split(',').filter(Boolean)
      if (ids.length === 0) {
        return h('span', { class: 'ops-tag dim' }, t('devices.gateway_none'))
      }
      return h(NSpace, { size: [4, 4], wrap: true }, () => ids.map(id => {
        const app = northApps.value.find(a => a.id === id)
        const name = app ? app.name : id.slice(0, 8)
        return h('span', { class: 'ops-tag cyan' }, name)
      }))
    }
  },
  {
    title: t('devices.col_status'),
    key: 'status',
    width: 100,
    render: (g: Group) => {
      if (!g.enabled) return h('span', { class: 'ops-state dead' }, t('devices.status_off'))
      const c = statusMap.value[g.id] === true
      return h('span', { class: 'ops-state ' + (c ? 'live' : 'dead') }, t(c ? 'devices.status_live' : 'devices.status_off'))
    }
  },
  {
    title: t('devices.col_actions'),
    key: 'action',
    width: 340,
    render: (g: Group) => h(NSpace, {}, () => [
      h(NButton, { class: 'ops-mini-button', size: 'small', type: 'info', onClick: () => router.push(`/groups/${g.id}`) }, () => t('devices.action_tags')),
      h(NButton, { class: 'ops-mini-button', size: 'small', onClick: () => openEdit(g.id) }, () => t('devices.action_edit')),
      h(NButton, { class: 'ops-mini-button', size: 'small', type: 'warning', onClick: () => onReload(g.id) }, () => t('devices.action_reload')),
      h(NButton, { class: 'ops-mini-button', size: 'small', type: 'error', onClick: () => onDelete(g.id) }, () => t('devices.action_del'))
    ])
  }
]
</script>

<template>
  <div class="ops-page">
    <div class="ops-heading">
      <div>
        <h1 class="ops-title">{{ t('devices.title') }}</h1>
        <p class="ops-subtitle">{{ t('devices.subtitle') }}</p>
      </div>
      <div class="ops-actions">
        <button class="ops-button primary" @click="openCreate">{{ t('devices.register') }}</button>
        <button class="ops-button" @click="refresh">{{ t('devices.refresh') }}</button>
        <button class="ops-button" @click="router.push('/northbound')">{{ t('devices.gw_config') }}</button>
      </div>
    </div>
    <section class="ops-panel health-panel">
      <div class="health-strip compact">
        <div><span>{{ t('groups.total_count') }}</span><strong>{{ groups.length }}</strong><small>{{ t('groups.enabled') }} <b>{{ enabledCount }}</b></small></div>
        <div><span>{{ t('groups.online_running') }}</span><strong>{{ connectedCount }}</strong><small>{{ t('groups.offline') }} {{ Math.max(enabledCount - connectedCount, 0) }}</small></div>
        <div><span>{{ t('groups.associated_conns') }}</span><strong>{{ connections.length }}</strong><small>{{ t('groups.shared_desc') }}</small></div>
        <div><span>{{ t('groups.bound_north') }}</span><strong>{{ linkedCount }}</strong><small>{{ t('groups.bound_desc') }}</small></div>
      </div>
    </section>
    <div class="ops-table-card">
      <n-data-table :columns="cols" :data="groups" :bordered="false" :pagination="false" size="small" />
    </div>

    <!-- 新建 -->
    <n-modal v-model:show="showCreate" class="ops-dialog" preset="dialog" :title="t('devices.register_title')" :positive-text="t('devices.register_btn')" :negative-text="t('devices.cancel')" @positive-click="onCreate">
      <n-form :model="formNew" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('devices.name')" :required="true"><n-input v-model:value="formNew.name" /></n-form-item>
        <n-form-item :label="t('devices.connection')" :required="true">
          <n-select :value="formNew.connectionId" :options="connectionOptions" @update:value="onConnectionChange(formNew, $event)" />
        </n-form-item>
        <n-form-item :label="t('devices.interval')" :required="true"><n-input-number v-model:value="formNew.intervalMs" :min="100" /></n-form-item>
        <dynamic-config-form v-model="formNew.config!" :schema="schemaFor(formNew.driver)" />
        <n-form-item :label="t('devices.gateway')">
          <div class="ops-gateway-grid">
            <div 
              v-for="app in northApps" 
              :key="app.id" 
              class="ops-gateway-card"
              :class="{ active: isAppSelected(formNew, app.id) }"
            >
              <div class="gateway-card-header" @click="toggleAppSelection(formNew, app.id)">
                <n-checkbox :checked="isAppSelected(formNew, app.id)" @update:checked="toggleAppSelection(formNew, app.id)" />
                <div class="gateway-info">
                  <strong class="gateway-name">{{ app.name }}</strong>
                  <span class="gateway-type">{{ app.type }}</span>
                </div>
                <div class="gateway-status">
                  <span v-if="app.connected" class="ops-state live">{{ t('gw.state_online') }}</span>
                  <span v-else-if="app.running && app.lastError" class="ops-state err" :title="app.lastError">{{ t('gw.state_err') }}</span>
                  <span v-else-if="app.running" class="ops-state warn">{{ t('gw.state_starting') }}</span>
                  <span v-else class="ops-state dead">{{ t('gw.state_off') }}</span>
                </div>
              </div>
              
              <div 
                v-if="isAppSelected(formNew, app.id) && app.type === 'jetlinks-mqtt'" 
                class="gateway-card-body"
              >
                <div class="gateway-field-row">
                  <div class="field-label">{{ t('devices.product_id') }} *</div>
                  <n-input v-model:value="formNew.device!.productId" :placeholder="t('devices.product_id_placeholder')" size="small" />
                </div>
                <div class="gateway-field-row">
                  <div class="field-label">{{ t('devices.device_id') }} *</div>
                  <n-input v-model:value="formNew.device!.deviceId" :placeholder="t('devices.device_id_placeholder')" size="small" />
                </div>
              </div>
            </div>
            <div v-if="!northApps.length" class="empty-gateway">{{ t('empty.north_apps') }}</div>
          </div>
        </n-form-item>
        <n-p depth="3" class="ops-modal-note">{{ t('devices.tooltip') }}</n-p>
      </n-form>
    </n-modal>

    <!-- 编辑 -->
    <n-modal v-model:show="showEdit" class="ops-dialog" preset="dialog" :title="t('devices.edit_title')" :positive-text="t('devices.save')" :negative-text="t('devices.cancel')" @positive-click="onSaveEdit">
      <n-form v-if="editingId" :model="formEdit" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('devices.name')" :required="true"><n-input v-model:value="formEdit.name" /></n-form-item>
        <n-form-item :label="t('devices.desc')"><n-input v-model:value="formEdit.description" /></n-form-item>
        <n-form-item :label="t('devices.connection')" :required="true">
          <n-select :value="formEdit.connectionId" :options="connectionOptions" @update:value="onConnectionChange(formEdit, $event)" />
        </n-form-item>
        <n-form-item :label="t('devices.interval')" :required="true"><n-input-number v-model:value="formEdit.intervalMs" :min="100" /></n-form-item>
        <n-form-item :label="t('gw.enabled')"><n-switch v-model:value="formEdit.enabled" /></n-form-item>
        <dynamic-config-form v-model="formEdit.config!" :schema="schemaFor(formEdit.driver)" />
        <n-form-item :label="t('devices.gateway')">
          <div class="ops-gateway-grid">
            <div 
              v-for="app in northApps" 
              :key="app.id" 
              class="ops-gateway-card"
              :class="{ active: isAppSelected(formEdit, app.id) }"
            >
              <div class="gateway-card-header" @click="toggleAppSelection(formEdit, app.id)">
                <n-checkbox :checked="isAppSelected(formEdit, app.id)" @update:checked="toggleAppSelection(formEdit, app.id)" />
                <div class="gateway-info">
                  <strong class="gateway-name">{{ app.name }}</strong>
                  <span class="gateway-type">{{ app.type }}</span>
                </div>
                <div class="gateway-status">
                  <span v-if="app.connected" class="ops-state live">{{ t('gw.state_online') }}</span>
                  <span v-else-if="app.running && app.lastError" class="ops-state err" :title="app.lastError">{{ t('gw.state_err') }}</span>
                  <span v-else-if="app.running" class="ops-state warn">{{ t('gw.state_starting') }}</span>
                  <span v-else class="ops-state dead">{{ t('gw.state_off') }}</span>
                </div>
              </div>
              
              <div 
                v-if="isAppSelected(formEdit, app.id) && app.type === 'jetlinks-mqtt'" 
                class="gateway-card-body"
              >
                <div class="gateway-field-row">
                  <div class="field-label">{{ t('devices.product_id') }} *</div>
                  <n-input v-model:value="formEdit.device!.productId" :placeholder="t('devices.product_id_placeholder')" size="small" />
                </div>
                <div class="gateway-field-row">
                  <div class="field-label">{{ t('devices.device_id') }} *</div>
                  <n-input v-model:value="formEdit.device!.deviceId" :placeholder="t('devices.device_id_placeholder')" size="small" />
                </div>
              </div>
            </div>
            <div v-if="!northApps.length" class="empty-gateway">{{ t('empty.north_apps') }}</div>
          </div>
        </n-form-item>
      </n-form>
    </n-modal>
  </div>
</template>

<style scoped>
.health-panel { margin-bottom: 12px; }.health-strip.compact { display: grid; grid-template-columns: repeat(4, 1fr); }.health-strip.compact > div { min-height: 96px; padding: 16px; border-right: 1px solid var(--line); }.health-strip.compact > div:last-child { border-right: 0; }.health-strip span, .health-strip small { display: block; color: var(--muted); font-size: 13px; }.health-strip strong { display: block; margin: 9px 0 8px; color: #eef4f6; font: 650 28px/1 var(--mono); }.health-strip small b { color: var(--cyan); }
@media (max-width: 900px) { .health-strip.compact { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 620px) { .health-strip.compact { grid-template-columns: 1fr; }.ops-actions { flex-wrap: wrap; } }

.ops-gateway-grid { display: flex; flex-direction: column; gap: 8px; width: 100%; margin-top: 4px; }
.ops-gateway-card { border: 1px solid var(--line); border-radius: 6px; background: rgba(255, 255, 255, 0.01); transition: all 0.2s ease; overflow: hidden; }
.ops-gateway-card:hover { border-color: rgba(49, 197, 231, 0.25); background: rgba(255, 255, 255, 0.02); }
.ops-gateway-card.active { border-color: var(--cyan); background: rgba(49, 197, 231, 0.03); }
.gateway-card-header { display: flex; align-items: center; gap: 12px; padding: 10px 14px; cursor: pointer; user-select: none; }
.gateway-info { display: flex; flex-direction: column; gap: 1px; }
.gateway-name { font-size: 13.5px; color: var(--text); font-weight: 550; line-height: 1.2; }
.gateway-type { font-size: 11px; color: var(--muted); }
.gateway-status { margin-left: auto; }
.gateway-card-body { padding: 10px 14px 12px 38px; display: flex; flex-direction: column; gap: 8px; border-top: 1px dashed var(--line); background: rgba(0, 0, 0, 0.15); }
html[data-theme='light'] .gateway-card-body { background: rgba(0, 0, 0, 0.02); }
.gateway-field-row { display: flex; align-items: center; gap: 12px; }
.gateway-field-row .field-label { width: 110px; font-size: 12.5px; color: #93a7b3; flex-shrink: 0; }
html[data-theme='light'] .gateway-field-row .field-label { color: #64748b; }
.empty-gateway { padding: 16px; text-align: center; color: var(--muted); border: 1px dashed var(--line); border-radius: 6px; }
</style>
