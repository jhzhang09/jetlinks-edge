<script setup lang="ts">
import { onMounted, onUnmounted, ref, h, computed } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NInput, NInputNumber, NSelect, NSpace, NTree, NSpin, useMessage, useDialog } from 'naive-ui'
import { getGroup, listTags, listDriverExtensions, createTag, updateTag, deleteTag, readTag, writeTag, lastValues, configDefaults, browseOPCUA, type ExtensionDescriptor, type Group, type Tag } from '@/api'
import { useI18n } from '@/i18n'
import DynamicConfigForm from '@/components/DynamicConfigForm.vue'

const { t, lang } = useI18n()
const route = useRoute(); const msg = useMessage(); const dialog = useDialog()
const groupId = computed(() => route.params.id as string)
const group = ref<Group | null>(null)
const tags = ref<Tag[]>([])
const extensions = ref<ExtensionDescriptor[]>([])
const liveValues = ref<Record<string, any>>({})
let timer: number | null = null

const TYPES = ['bool','int16','uint16','int32','uint32','int64','uint64','float32','float64','string','bytes']
const showCreate = ref(false)
const editing = ref<Partial<Tag> | null>(null)
const form = ref<Partial<Tag>>({ name:'', type:'int16', access:'ro', decimal:1, precision:0, config:{} })

const showBrowse = ref(false)
const browseLoading = ref(false)
const browseNodes = ref<any[]>([])
const selectedNodes = ref<any[]>([])
const expandedKeys = ref<string[]>([])
const nodeMap = new Map<string, any>()

function findNodeById(nodes: any[], id: string): any {
  for (const node of nodes) {
    if (node.id === id) return node
    if (node.children) {
      const found = findNodeById(node.children, id)
      if (found) return found
    }
  }
  return null
}

async function openBrowse() {
  browseNodes.value = []
  selectedNodes.value = []
  expandedKeys.value = []
  nodeMap.clear()
  showBrowse.value = true
  browseLoading.value = true
  try {
    const res = await browseOPCUA(groupId.value)
    browseNodes.value = (res.items || []).map(item => {
      nodeMap.set(item.id, item)
      return {
        id: item.id,
        name: item.name,
        folder: item.folder,
        type: item.type,
        children: item.folder ? [{ id: item.id + '_placeholder', name: 'Loading...', folder: false, placeholder: true }] : undefined
      }
    })
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  } finally {
    browseLoading.value = false
  }
}

async function handleExpand(keys: string[]) {
  expandedKeys.value = keys
  for (const key of keys) {
    const node = findNodeById(browseNodes.value, key)
    if (node && node.folder && node.children && node.children.length === 1 && node.children[0].placeholder) {
      try {
        const res = await browseOPCUA(groupId.value, node.id)
        node.children = (res.items || []).map((item: any) => {
          nodeMap.set(item.id, item)
          return {
            id: item.id,
            name: item.name,
            folder: item.folder,
            type: item.type,
            children: item.folder ? [{ id: item.id + '_placeholder', name: 'Loading...', folder: false, placeholder: true }] : undefined
          }
        })
        browseNodes.value = [...browseNodes.value]
      } catch (err: any) {
        msg.error(err.message || t('common.load_failed'))
      }
    }
  }
}

function handleSelect(keys: any) {
  selectedNodes.value = keys
}

async function addSelectedNodes() {
  const variableKeys = selectedNodes.value.filter(key => {
    const item = nodeMap.get(key)
    return item && !item.folder
  })
  
  if (variableKeys.length === 0) {
    msg.warning(t('tags.opcua_select_required'))
    return
  }
  
  browseLoading.value = true
  try {
    let successCount = 0
    for (const key of variableKeys) {
      const item = nodeMap.get(key)
      if (!item) continue
      
      const exists = tags.value.some(t => t.name === item.name || t.config?.nodeId === item.id)
      if (exists) continue
      
      await createTag(groupId.value, {
        name: item.name,
        type: item.type,
        access: 'ro',
        decimal: 1,
        precision: 0,
        config: {
          nodeId: item.id
        }
      })
      successCount++
    }
    msg.success(t('tags.opcua_batch_success').replace('{count}', String(successCount)))
    showBrowse.value = false
    refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.create_failed'))
  } finally {
    browseLoading.value = false
  }
}

const browseCols = computed(() => {
  return [
    {
      type: 'selection',
      disabled(row: any) {
        if (row.folder || row.placeholder) return true
        return tags.value.some(t => t.config?.nodeId === row.id)
      }
    },
    {
      title: t('tags.opcua_node_name'),
      key: 'name',
      render(row: any) {
        if (row.placeholder) {
          return h('span', { style: 'color: var(--dim); font-style: italic;' }, 'Loading...')
        }
        if (row.folder) {
          return h('span', { style: 'color: var(--cyan); font-weight: 600;' }, row.name)
        }
        const alreadyExists = tags.value.some(t => t.config?.nodeId === row.id)
        if (alreadyExists) {
          return h('span', { style: 'color: var(--dim);' }, [
            row.name,
            h('span', { class: 'ops-tag dim', style: 'margin-left:6px' }, t('tags.opcua_added'))
          ])
        }
        return h('span', {}, row.name)
      }
    },
    {
      title: 'Node ID',
      key: 'id',
      render(row: any) {
        if (row.placeholder) return '—'
        return h('code', { class: 'ops-mono', style: 'font-size:11px;color:var(--amber)' }, row.id)
      }
    },
    {
      title: t('tags.opcua_class'),
      key: 'folder',
      render(row: any) {
        if (row.placeholder) return '—'
        return h('span', { class: 'ops-tag ' + (row.folder ? 'cyan' : 'amber') }, row.folder ? 'Folder' : 'Variable')
      }
    },
    {
      title: t('tags.opcua_data_type'),
      key: 'type',
      render(row: any) {
        if (row.placeholder || row.folder) return '—'
        return h('code', {}, row.type)
      }
    }
  ]
})


async function refresh() { group.value = await getGroup(groupId.value); tags.value = (await listTags(groupId.value)).items || [] }
async function loadExtensions() { extensions.value = (await listDriverExtensions()).items || [] }
const tagSchema = computed(() => extensions.value.find(item => item.type === group.value?.driver)?.tagSchema || [])
function openCreate() {
  form.value = { name:'', type:'int16', access:'ro', decimal:1, precision:0, config:configDefaults(tagSchema.value) }
  showCreate.value = true
}
async function refreshValues() { liveValues.value = await lastValues(groupId.value) }
onMounted(() => { refresh(); loadExtensions(); refreshValues(); timer = window.setInterval(refreshValues, 1000) })
onUnmounted(() => { if (timer) clearInterval(timer) })

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
  if (!form.value.name || !form.value.type) { msg.warning(t('tags.required')); return }
  if (!validateConfig(form.value.config, tagSchema.value)) return
  try {
    await createTag(groupId.value, form.value); msg.success(t('tags.added')); showCreate.value = false;
    form.value = { name:'', type:'int16', access:'ro', decimal:1, precision:0, config:configDefaults(tagSchema.value) }
    refresh()
  } catch (err: any) {
    msg.error(err.message || t('common.create_failed'))
  }
}
function startEdit(tg: Tag) { editing.value = { ...tg, config: { ...(tg.config || {}) } } }
async function saveEdit() {
  if (!editing.value?.id) return
  if (!validateConfig(editing.value?.config, tagSchema.value)) return
  try {
    await updateTag(editing.value.id, editing.value); msg.success(t('tags.updated')); editing.value = null; refresh()
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
        await deleteTag(id)
        msg.success(t('tags.removed'))
        refresh()
      } catch (err: any) {
        msg.error(err.message || t('common.delete_failed'))
      }
    }
  })
}
async function onRead(id: string) {
  try {
    const r = await readTag(id); msg.info(`${t('tags.val')}: ${JSON.stringify(r.value)} [${r.quality}]`)
  } catch (err: any) {
    msg.error(err.message || t('common.load_failed'))
  }
}
const writeValue = ref(''); const writeTarget = ref<string | null>(null)
async function onWrite(id: string) {
  if (writeTarget.value !== id) { writeTarget.value = id; writeValue.value = ''; return }
  let val: any = writeValue.value
  if (val === 'true') val = true; else if (val === 'false') val = false; else if (!isNaN(Number(val)) && val !== '') val = Number(val)
  try {
    await writeTag(id, val); msg.success(t('tags.write_ok')); writeTarget.value = null
  } catch (err: any) {
    msg.error(err.message || t('common.save_failed'))
  }
}
const qTag = (q: string) => t(q === 'good' ? 'tags.quality_good' : 'tags.quality_bad')
const readableCount = computed(() => tags.value.filter(item => item.access === 'ro' || item.access === 'rw').length)
const writableCount = computed(() => tags.value.filter(item => item.access === 'wo' || item.access === 'rw').length)
const liveValueCount = computed(() => Object.keys(liveValues.value).length)
function getSubtitle() {
  if (group.value?.driver === 'opc-ua') {
    return t('tags.opcua_title')
  }
  return t('tags.subtitle')
}

const cols = computed(() => {
  const isOpcUA = group.value?.driver === 'opc-ua'
  
  const baseCols = [
    { title: t('tags.tag_name'), key: 'name', width: 130, render: (tg: Tag) => h('span', { style: 'color:var(--cyan);font-weight:600;font-size:12px' }, tg.name) },
    { 
      title: isOpcUA ? 'Node ID' : t('tags.col_addr'), 
      key: 'address', 
      width: isOpcUA ? 220 : 100, 
      render: (tg: Tag) => h('code', { class: 'ops-mono', style: 'color:var(--amber);font-size:11px' }, isOpcUA ? (tg.config?.nodeId || '—') : tg.address)
    },
    { title: t('tags.col_type'), key: 'type', width: 80, render: (tg: Tag) => h('code', {}, tg.type) },
  ]
  
  if (!isOpcUA) {
    baseCols.push({ title: t('tags.col_order'), key: 'byteOrder', width: 70, render: (tg: Tag) => h('code', {}, tg.byteOrder) })
  }
  
  baseCols.push(
    { title: t('tags.col_live'), key: 'live', width: 220,
      render: (tg: Tag) => {
        const v = liveValues.value[tg.id]
        if (!v) return h('span', { style: 'color:var(--dim)' }, '—')
        const cls = v.quality === 'good' ? 'ops-state live' : 'ops-state err'
        return h('span', { class: cls }, JSON.stringify(v.value)+'  '+qTag(v.quality))
      }
    },
    { 
      title: t('tags.col_update_time'), 
      key: 'updateTime', 
      width: 120,
      render: (tg: Tag) => {
        const v = liveValues.value[tg.id]
        if (!v || !v.time) return h('span', { style: 'color:var(--dim)' }, '—')
        try {
          const d = new Date(v.time)
          const formatPad = (n: number) => n.toString().padStart(2, '0')
          return h('code', { class: 'ops-mono', style: 'color:#8a9baa;font-size:11px;' }, `${formatPad(d.getHours())}:${formatPad(d.getMinutes())}:${formatPad(d.getSeconds())}`)
        } catch {
          return h('span', { style: 'color:var(--dim)' }, '—')
        }
      }
    },
    { title: t('devices.col_actions'), key: 'action', width: 340,
      render: (tg: Tag) => h(NSpace, {}, () => [
        h(NButton, { class: 'ops-mini-button', size: 'small', onClick: () => startEdit(tg) }, () => t('tags.edit')),
        h(NButton, { class: 'ops-mini-button', size: 'small', type: 'info', onClick: () => onRead(tg.id) }, () => t('tags.read')),
        writeTarget.value === tg.id
          ? h(NInput, { size: 'small', value: writeValue.value, 'onUpdate:value': (v: string) => writeValue.value = v, placeholder: t('tags.val'), onKeyup: (e: KeyboardEvent) => { if (e.key === 'Enter') onWrite(tg.id) }, style: 'width:120px' })
          : h(NButton, { class: 'ops-mini-button', size: 'small', type: 'warning', onClick: () => onWrite(tg.id) }, () => t('tags.write')),
        h(NButton, { class: 'ops-mini-button', size: 'small', type: 'error', onClick: () => onDelete(tg.id) }, () => t('tags.del'))
      ])
    }
  )
  
  return baseCols
})
</script>

<template>
  <div class="ops-page">
    <div class="ops-heading">
      <div>
        <h1 class="ops-title">{{ group?.name || '—' }}</h1>
        <p class="ops-subtitle">{{ getSubtitle() }}</p>
      </div>
      <div class="ops-actions">
        <button v-if="group?.driver === 'opc-ua'" class="ops-button primary" @click="openBrowse">{{ t('tags.opcua_browse') }}</button>
        <button class="ops-button primary" @click="openCreate">{{ t('tags.add') }}</button>
        <button class="ops-button" @click="refresh">{{ t('tags.refresh') }}</button>
      </div>
    </div>
    <section class="ops-panel health-panel">
      <div class="health-strip compact">
        <div><span>{{ t('tags.total_count') }}</span><strong>{{ tags.length }}</strong><small>{{ group?.driver || '—' }}</small></div>
        <div><span>{{ t('tags.readable') }}</span><strong>{{ readableCount }}</strong><small>{{ t('tags.live_polling') }} {{ liveValueCount }}</small></div>
        <div><span>{{ t('tags.writable') }}</span><strong>{{ writableCount }}</strong><small>{{ t('tags.write_support') }}</small></div>
        <div><span>{{ t('tags.plugin_fields') }}</span><strong>{{ tagSchema.length }}</strong><small>{{ t('tags.dynamic_render') }}</small></div>
      </div>
    </section>
    <div class="ops-table-card">
      <n-data-table :columns="cols" :data="tags" :bordered="false" :pagination="false" size="small" />
    </div>

    <n-modal v-model:show="showCreate" class="ops-dialog" preset="dialog" :title="t('tags.add_title')" :positive-text="t('tags.add_btn')" :negative-text="t('tags.cancel')" @positive-click="onCreate">
      <n-form :model="form" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('tags.tag_name')" :required="true"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item :label="t('tags.type')" :required="true"><n-select v-model:value="form.type" :options="TYPES.map(x=>({label:x,value:x}))" /></n-form-item>
        <n-form-item :label="t('tags.decimal')"><n-input-number v-model:value="form.decimal" :step="0.1" /></n-form-item>
        <n-form-item :label="t('tags.access')"><n-select v-model:value="form.access" :options="['ro','rw','wo'].map(a=>({label:a,value:a}))" /></n-form-item>
        <n-form-item :label="t('tags.desc')"><n-input v-model:value="form.description" /></n-form-item>
        <dynamic-config-form v-model="form.config!" :schema="tagSchema" />
      </n-form>
    </n-modal>

    <n-modal :show="!!editing" class="ops-dialog" @update:show="(v:boolean)=>{if(!v) editing=null}" preset="dialog" :title="t('tags.edit_title')" :positive-text="t('tags.save')" :negative-text="t('tags.cancel')" @positive-click="saveEdit">
      <n-form v-if="editing" :model="editing" label-placement="left" label-width="160" size="small">
        <n-form-item :label="t('tags.tag_name')" :required="true"><n-input v-model:value="editing!.name" /></n-form-item>
        <n-form-item :label="t('tags.type')" :required="true"><n-select v-model:value="editing!.type" :options="TYPES.map(x=>({label:x,value:x}))" /></n-form-item>
        <n-form-item :label="t('tags.decimal')"><n-input-number v-model:value="editing!.decimal" :step="0.1" /></n-form-item>
        <n-form-item :label="t('tags.access')"><n-select v-model:value="editing!.access" :options="['ro','rw','wo'].map(a=>({label:a,value:a}))" /></n-form-item>
        <n-form-item :label="t('tags.desc')"><n-input v-model:value="editing!.description" /></n-form-item>
        <dynamic-config-form v-model="editing!.config!" :schema="tagSchema" />
      </n-form>
    </n-modal>

    <!-- 浏览 OPC UA 节点的 Modal -->
    <n-modal v-model:show="showBrowse" class="ops-dialog ops-dialog-wide" preset="dialog" :title="t('tags.opcua_title')">
      <div class="browse-head">
        <span class="ops-modal-note">
          {{ t('tags.opcua_select') }}
        </span>
        <button class="ops-button primary" :disabled="selectedNodes.length === 0 || browseLoading" @click="addSelectedNodes">
          {{ t('tags.opcua_batch_add').replace('{count}', String(selectedNodes.length)) }}
        </button>
      </div>
      <n-spin :show="browseLoading">
        <div class="ops-scroll-box ops-table-card">
          <n-data-table 
            :columns="browseCols" 
            :data="browseNodes" 
            :bordered="true" 
            :pagination="false" 
            size="small"
            :row-key="(row: any) => row.id"
            :expanded-row-keys="expandedKeys"
            @update:expanded-row-keys="handleExpand"
            @update:checked-row-keys="handleSelect"
          />
        </div>
      </n-spin>
    </n-modal>
  </div>
</template>

<style scoped>
.health-panel { margin-bottom: 12px; }.health-strip.compact { display: grid; grid-template-columns: repeat(4, 1fr); }.health-strip.compact > div { min-height: 96px; padding: 16px; border-right: 1px solid var(--line); }.health-strip.compact > div:last-child { border-right: 0; }.health-strip span, .health-strip small { display: block; color: var(--muted); font-size: 13px; }.health-strip strong { display: block; margin: 9px 0 8px; color: #eef4f6; font: 650 28px/1 var(--mono); }.browse-head { margin-bottom: 12px; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
@media (max-width: 900px) { .health-strip.compact { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 620px) { .health-strip.compact { grid-template-columns: 1fr; }.ops-actions, .browse-head { flex-wrap: wrap; } }
</style>
