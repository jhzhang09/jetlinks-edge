<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { NButton, NDataTable, useMessage, type DataTableColumns } from 'naive-ui'
import { listExternalPlugins, reloadExternalPlugins, type ExternalPlugin } from '@/api'
import { useI18n } from '@/i18n'

const { t } = useI18n()
const message = useMessage()
const plugins = ref<ExternalPlugin[]>([])
const directory = ref('')
const loading = ref(false)

const driverCount = computed(() => plugins.value.filter(item => item.kind === 'driver').length)
const northCount = computed(() => plugins.value.filter(item => item.kind === 'north').length)

async function refresh() {
  loading.value = true
  try {
    const result = await listExternalPlugins()
    plugins.value = result.items || []
    directory.value = result.directory
  } catch (error: any) {
    message.error(error?.response?.data?.error || error?.message || t('plugins.load_failed'))
  } finally {
    loading.value = false
  }
}

async function reload() {
  loading.value = true
  try {
    const result = await reloadExternalPlugins()
    plugins.value = result.items || []
    const changed = result.changes.driverTypes.length + result.changes.northTypes.length
    message.success(t('plugins.reloaded').replace('{count}', String(changed)))
  } catch (error: any) {
    message.error(error?.response?.data?.error || error?.message || t('plugins.reload_failed'))
  } finally {
    loading.value = false
  }
}

const columns: DataTableColumns<ExternalPlugin> = [
  {
    title: t('plugins.name'),
    key: 'name',
    render: plugin => h('div', { class: 'plugin-name' }, [
      h('strong', plugin.descriptor.name),
      h('code', plugin.descriptor.type)
    ])
  },
  {
    title: t('plugins.direction'),
    key: 'kind',
    width: 140,
    render: plugin => h('span', { class: `ops-tag ${plugin.kind === 'driver' ? 'cyan' : 'amber'}` }, t(`plugins.kind_${plugin.kind}`))
  },
  {
    title: t('plugins.version'),
    key: 'version',
    width: 130,
    render: plugin => h('code', { class: 'ops-mono' }, plugin.descriptor.version)
  },
  {
    title: t('plugins.capabilities'),
    key: 'capabilities',
    render: plugin => h('div', { class: 'capabilities' }, (plugin.descriptor.capabilities || []).map(value => h('span', { class: 'ops-tag dim' }, value)))
  },
  {
    title: t('plugins.manifest'),
    key: 'manifest',
    width: 220,
    render: plugin => h('code', { class: 'ops-mono' }, plugin.manifest)
  }
]

onMounted(refresh)
</script>

<template>
  <div class="ops-page">
    <div class="ops-heading">
      <div>
        <p class="ops-kicker">RUNTIME EXTENSIONS</p>
        <h1 class="ops-title">{{ t('plugins.title') }}</h1>
        <p class="ops-subtitle">{{ t('plugins.subtitle') }}</p>
      </div>
      <div class="ops-actions">
        <n-button class="ops-mini-button" :loading="loading" @click="refresh">{{ t('plugins.refresh') }}</n-button>
        <n-button type="primary" :loading="loading" @click="reload">{{ t('plugins.reload') }}</n-button>
      </div>
    </div>

    <section class="plugin-overview">
      <div><span>{{ t('plugins.directory') }}</span><code>{{ directory || '—' }}</code></div>
      <div><span>{{ t('plugins.driver_count') }}</span><strong>{{ driverCount }}</strong></div>
      <div><span>{{ t('plugins.north_count') }}</span><strong>{{ northCount }}</strong></div>
      <div><span>{{ t('plugins.protocol') }}</span><code>jetlinks-edge-plugin/v1</code></div>
    </section>

    <div class="ops-table-card">
	  <n-data-table :columns="columns" :data="plugins" :loading="loading" :bordered="false" :pagination="false">
		<template #empty>
		  <div class="plugin-empty">
			<strong>{{ t('plugins.empty_title') }}</strong>
			<span>{{ t('plugins.empty_body') }}</span>
		  </div>
		</template>
	  </n-data-table>
    </div>
  </div>
</template>

<style scoped>
.plugin-overview { display: grid; grid-template-columns: minmax(280px, 2fr) repeat(3, minmax(150px, 1fr)); gap: 1px; margin-bottom: 12px; border: 1px solid var(--line); background: var(--line); }
.plugin-overview > div { min-height: 84px; display: flex; flex-direction: column; justify-content: center; gap: 10px; padding: 14px 16px; background: var(--surface); }
.plugin-overview span { color: var(--muted); font-size: 12px; letter-spacing: .06em; text-transform: uppercase; }
.plugin-overview strong { color: var(--text-strong); font: 650 28px/1 var(--mono); }
.plugin-overview code { color: var(--cyan); font: 12px var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.plugin-name) { display: flex; flex-direction: column; gap: 3px; }
:deep(.plugin-name strong) { color: var(--text-strong); font-weight: 650; }
:deep(.plugin-name code) { color: var(--muted); font: 11px var(--mono); }
:deep(.capabilities) { display: flex; flex-wrap: wrap; gap: 5px; }
.plugin-empty { min-height: 300px; display: grid; place-content: center; gap: 8px; text-align: center; color: var(--muted); }
.plugin-empty strong { color: var(--text-strong); font-size: 15px; }
@media (max-width: 900px) { .plugin-overview { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { .plugin-overview { grid-template-columns: 1fr; } }
</style>
