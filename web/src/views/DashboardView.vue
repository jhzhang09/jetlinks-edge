<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useOperations } from '@/composables/useOperations'
import { useI18n } from '@/i18n'
import OpsTrendChart from '@/components/OpsTrendChart.vue'
import { formatGoDateTime } from '@/utils/time'

const router = useRouter()
const { t } = useI18n()
const { data, error, connectedConnections, enabledConnections, connectedGroups, enabledGroups, connectedNorth, enabledNorth, criticalAlarms, overallHealth, history, refresh } = useOperations()
const warningAlarms = computed(() => data.value.alarms.filter(item => item.severity === 'warning').length)
const ioCount = computed(() => data.value.groups.reduce((total, group) => total + Object.entries(group.stats || {}).reduce((sum, [key, value]) => key.startsWith('ok.') ? sum + value : sum, 0), 0))
const ioErrors = computed(() => data.value.groups.reduce((total, group) => total + (group.stats?.err || 0), 0))
const health = overallHealth
const time = formatGoDateTime

function getPluginStyle(type: string) {
  const t = type.toLowerCase()
  if (t.includes('modbus')) {
    return { bg: 'linear-gradient(135deg, #4ad7a2 0%, #208e67 100%)', color: '#070b10' }
  } else if (t.includes('mqtt')) {
    return { bg: 'linear-gradient(135deg, #31c5e7 0%, #177a90 100%)', color: '#070b10' }
  } else if (t.includes('opc')) {
    return { bg: 'linear-gradient(135deg, #f0b54d 0%, #a4721c 100%)', color: '#070b10' }
  } else if (t.includes('http') || t.includes('webhook')) {
    return { bg: 'linear-gradient(135deg, #f06473 0%, #ab3744 100%)', color: '#070b10' }
  } else if (t.includes('tcp') || t.includes('udp')) {
    return { bg: 'linear-gradient(135deg, #a78bfa 0%, #6d28d9 100%)', color: '#070b10' }
  }
  return { bg: 'linear-gradient(135deg, #738896 0%, #3a4b56 100%)', color: '#070b10' }
}
</script>

<template>
  <div class="ops-page dashboard">
    <div class="ops-heading"><div><h1 class="ops-title">{{ t('dash.overview') }}</h1><p class="ops-subtitle">{{ t('dash.subtitle_main') }}</p></div><div class="ops-actions"><button class="ops-button" @click="refresh">{{ t('devices.refresh') }}</button><button class="ops-button" @click="router.push('/groups')">{{ t('nav.devices') }}</button><button class="ops-button primary" @click="router.push('/topology')">{{ t('dash.quick_ops') }}</button></div></div>
    <div v-if="error" class="ops-error">{{ error }}</div>

    <section class="ops-panel health-panel">
      <header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('dash.health_overview') }}</h2></header>
      <div class="health-strip">
        <div><span>{{ t('col.channels') }}</span><strong>{{ data.connections?.length || 0 }}</strong><small><b>{{ connectedConnections }}</b> {{ t('common.online') }}&nbsp;&nbsp;{{ (data.connections?.length || 0) - connectedConnections }} {{ t('common.offline') }}</small></div>
        <div><span>{{ t('dash.groups') }}</span><strong>{{ data.groups.length }}</strong><small><b>{{ connectedGroups }}</b> {{ t('common.online') }}&nbsp;&nbsp;{{ data.groups.length - connectedGroups }} {{ t('common.offline') }}</small></div>
        <div><span>{{ t('north.total_count') }}</span><strong>{{ data.northApps.length }}</strong><small><b>{{ connectedNorth }}</b> {{ t('common.online') }}&nbsp;&nbsp;{{ data.northApps.length - connectedNorth }} {{ t('common.offline') }}</small></div>
        <div class="health-score"><span>{{ t('dash.health_score') }}</span><strong>{{ health }}%</strong><small>{{ t('severity.critical') }} {{ criticalAlarms }} · {{ t('severity.warning') }} {{ warningAlarms }}</small></div>
        <div><span>{{ t('dash.data_collect') }}</span><strong>{{ ioCount }}</strong><small><b>{{ t('dash.success') }}</b>&nbsp;&nbsp;{{ t('dash.failed') }} {{ ioErrors }}</small></div>
        <div><span>{{ t('dash.gw_status') }}</span><strong class="cyan">{{ t('dash.status_normal') }}</strong><small>{{ t('dash.tasks_count').replace('{count}', String(enabledGroups)) }}</small></div>
      </div>
    </section>

    <div class="overview-middle">
      <section class="ops-panel trend-panel">
        <header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('dash.health_trend') }}</h2><span class="ops-panel-meta">{{ t('dash.realtime_snapshot') }}</span></header>
        <div class="legend"><span class="good">{{ t('dash.legend_health') }}</span><span class="warn">{{ t('dash.legend_warn') }}</span><span class="bad">{{ t('dash.legend_bad') }}</span></div>
        <div class="chart"><OpsTrendChart :healthy="history.healthy" :warning="history.warning" :critical="history.critical" /></div>
      </section>
      <section class="ops-panel active-alarms">
        <header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('dash.active_alarms_count') }} <b>{{ data.alarms.length }}</b></h2><button @click="router.push('/alarms')">{{ t('log.view_all') }}</button></header>
        <div class="alarm-rows"><button v-for="alarm in data.alarms.slice(0, 7)" :key="alarm.id" @click="router.push(alarm.route)"><b :class="alarm.severity">{{ t('severity.' + alarm.severity) }}</b><span>{{ alarm.message || t('alarms.evidence_desc').replace('{msg}', '') }}</span><small>{{ alarm.sourceName }}</small><time>{{ time(alarm.time) }}</time></button><div v-if="!data.alarms.length" class="ops-empty">{{ t('dash.no_active_alarms') }}</div></div>
      </section>
    </div>

    <section class="ops-panel plugins-panel">
      <header class="ops-panel-header">
        <h2 class="ops-panel-title">{{ t('dash.plugin_ecosystem') }}</h2>
        <span class="ops-panel-meta">{{ t('dash.loaded_plugins').replace('{south}', String(data.driverPlugins.length)).replace('{north}', String(data.northPlugins.length)) }}</span>
      </header>
      <div class="ops-panel-body plugins-body">
        <div class="plugin-section">
          <h3 class="plugin-section-title">{{ t('dash.south_drivers') }} <small>Southbound Drivers</small></h3>
          <div class="plugin-grid">
            <div v-for="plugin in data.driverPlugins" :key="plugin.type" class="plugin-card">
              <div class="plugin-card-icon">
                <div class="icon-avatar" :style="{ background: getPluginStyle(plugin.type).bg, color: getPluginStyle(plugin.type).color }">
                  {{ (plugin.name || '?').charAt(0).toUpperCase() }}
                </div>
              </div>
              <div class="plugin-card-content">
                <div class="plugin-title-row">
                  <span class="plugin-name" :title="plugin.name">{{ plugin.name }}</span>
                  <span class="plugin-version">v{{ plugin.version }}</span>
                </div>
                <p class="plugin-desc" :title="plugin.description">{{ plugin.description || t('dash.no_desc') }}</p>
                <div class="plugin-tags">
                  <span class="plugin-type-tag">{{ plugin.type }}</span>
                  <span v-for="cap in plugin.capabilities || []" :key="cap" class="plugin-cap-tag">{{ cap }}</span>
                </div>
              </div>
            </div>
            <div v-if="!data.driverPlugins.length" class="ops-empty min-h-60">{{ t('dash.empty_south') }}</div>
          </div>
        </div>

        <div class="plugin-section">
          <h3 class="plugin-section-title">{{ t('dash.north_channels') }} <small>Northbound Channels</small></h3>
          <div class="plugin-grid">
            <div v-for="plugin in data.northPlugins" :key="plugin.type" class="plugin-card">
              <div class="plugin-card-icon">
                <div class="icon-avatar" :style="{ background: getPluginStyle(plugin.type).bg, color: getPluginStyle(plugin.type).color }">
                  {{ (plugin.name || '?').charAt(0).toUpperCase() }}
                </div>
              </div>
              <div class="plugin-card-content">
                <div class="plugin-title-row">
                  <span class="plugin-name" :title="plugin.name">{{ plugin.name }}</span>
                  <span class="plugin-version">v{{ plugin.version }}</span>
                </div>
                <p class="plugin-desc" :title="plugin.description">{{ plugin.description || t('dash.no_desc') }}</p>
                <div class="plugin-tags">
                  <span class="plugin-type-tag">{{ plugin.type }}</span>
                  <span v-for="cap in plugin.capabilities || []" :key="cap" class="plugin-cap-tag">{{ cap }}</span>
                </div>
              </div>
            </div>
            <div v-if="!data.northPlugins.length" class="ops-empty min-h-60">{{ t('dash.empty_north') }}</div>
          </div>
        </div>
      </div>
    </section>

    <section class="ops-panel devices">
      <header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('dash.groups_overview') }}</h2><div class="table-tools"><span>{{ t('common.all') }} PROTO</span><span>{{ t('common.all') }} STATE</span><strong>{{ t('common.items_total').replace('{count}', String(data.groups.length)) }}</strong></div></header>
      <table class="ops-table"><thead><tr><th>{{ t('dash.col_status') }}</th><th>{{ t('dash.col_group_name') }}</th><th>{{ t('dash.col_ids') }}</th><th>{{ t('dash.col_proto') }}</th><th>{{ t('dash.col_interval') }}</th><th>{{ t('dash.col_last_time') }}</th><th>{{ t('dash.col_data') }}</th><th>{{ t('dash.col_actions') }}</th></tr></thead><tbody>
        <tr v-for="group in data.groups" :key="group.id" @click="router.push(`/groups/${group.id}`)"><td><b class="state" :class="group.connected ? 'live' : group.enabled ? 'dead' : 'warn'">{{ group.connected ? t('common.online') : group.enabled ? t('common.offline') : t('common.disable') }}</b></td><td class="main-cell">{{ group.name }}</td><td class="ops-mono">{{ group.deviceId || group.id }}</td><td>{{ group.driver }}</td><td>{{ group.intervalMs }} ms</td><td>{{ time(group.lastTime) }}</td><td class="cyan">{{ t('dash.tags_count').replace('{count}', String(group.valueCount)) }}</td><td>{{ t('common.detail') }} ···</td></tr>
      </tbody></table><div v-if="!data.groups.length" class="ops-empty">{{ t('dash.empty_groups') }}</div>
    </section>
  </div>
</template>

<style scoped>
.health-panel { margin-bottom: 12px; }.health-strip { display: grid; grid-template-columns: repeat(6, 1fr); }.health-strip > div { min-height: 118px; padding: 16px 18px; border-right: 1px solid var(--line); }.health-strip > div:last-child { border: 0; }.health-strip span, .health-strip small { display: block; color: var(--muted); font-size: 12px; }.health-strip strong { display: block; margin: 13px 0 10px; color: #eef4f6; font: 650 28px/1 var(--mono); }.health-strip small b, .cyan { color: var(--cyan); }.health-score strong { color: var(--green); }
.overview-middle { display: grid; grid-template-columns: 1.1fr .9fr; gap: 12px; margin-bottom: 12px; }.trend-panel, .active-alarms { min-height: 310px; }.legend { height: 36px; display: flex; align-items: center; gap: 20px; padding: 0 15px; color: var(--muted); font-size: 12px; }.legend span::before { content: ""; display: inline-block; width: 14px; height: 2px; margin-right: 6px; vertical-align: middle; background: currentColor; }.legend .good { color: var(--cyan); }.legend .warn { color: var(--amber); }.legend .bad { color: var(--red); }.chart { height: 225px; padding: 0 10px 10px; }
.active-alarms header button { border: 0; background: transparent; color: var(--cyan); font-size: 12px; cursor: pointer; }.active-alarms header b { color: white; background: var(--red); padding: 2px 6px; }.alarm-rows button { width: 100%; min-height: 42px; display: grid; grid-template-columns: 52px 1fr 110px 150px; align-items: center; gap: 8px; padding: 0 12px; border: 0; border-bottom: 1px solid var(--line); background: transparent; color: var(--muted); font-size: 12px; text-align: left; cursor: pointer; }.alarm-rows button:hover { background: #10202a; }.alarm-rows b { color: var(--amber); }.alarm-rows b.critical { color: var(--red); }.alarm-rows span { color: #b8c7ce; }.alarm-rows time { font: 12px var(--mono); }

/* 网关插件生态样式 */
.plugins-panel { margin-bottom: 12px; }
.plugins-body { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; padding: 16px; }
.plugin-section { min-width: 0; }
.plugin-section-title { margin: 0 0 14px 0; color: #b8c7cf; font-size: 14px; font-weight: 650; text-transform: uppercase; border-bottom: 1px dashed var(--line); padding-bottom: 6px; }
.plugin-section-title small { font-size: 12px; color: var(--dim); margin-left: 6px; text-transform: none; }
.plugin-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 10px; }
.plugin-card { display: flex; gap: 12px; padding: 12px; background: var(--surface-2); border: 1px solid var(--line); border-radius: 4px; transition: all 0.25s ease; min-width: 0; }
.plugin-card:hover { border-color: rgba(49, 197, 231, 0.45); background: var(--surface-3); box-shadow: 0 4px 16px rgba(49, 197, 231, 0.08); transform: translateY(-1px); }
.plugin-card-icon { flex-shrink: 0; }
.icon-avatar { width: 38px; height: 38px; border-radius: 50%; display: grid; place-items: center; font-weight: 700; font-size: 16px; font-family: var(--mono); transition: transform 0.25s ease; }
.plugin-card:hover .icon-avatar { transform: scale(1.05); }
.plugin-card-content { flex-grow: 1; min-width: 0; }
.plugin-title-row { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 5px; }
.plugin-name { color: #e7eef1; font-size: 14px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.plugin-version { font-size: 11px; font-family: var(--mono); color: var(--muted); border: 1px solid var(--line); padding: 0px 4px; border-radius: 2px; background: rgba(255,255,255,0.01); }
.plugin-desc { color: var(--muted); font-size: 13px; line-height: 1.4; margin: 0 0 10px 0; height: 38px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; text-overflow: ellipsis; }
.plugin-tags { display: flex; flex-wrap: wrap; gap: 5px; }
.plugin-type-tag { font-family: var(--mono); font-size: 11px; color: var(--muted); background: var(--surface-3); padding: 1px 4px; border-radius: 2px; border: 1px solid var(--line); }
.plugin-cap-tag { font-size: 11px; color: var(--cyan); background: rgba(49, 197, 231, 0.08); border: 1px solid rgba(49, 197, 231, 0.2); padding: 1px 4px; border-radius: 2px; text-transform: uppercase; }
.min-h-60 { min-height: 60px; }

.table-tools { display: flex; gap: 8px; align-items: center; }.table-tools span { padding: 7px 24px 7px 10px; border: 1px solid var(--line); color: var(--muted); font-size: 12px; }.table-tools strong { color: var(--muted); font-size: 12px; }.state { font-size: 12px; }.state.live { color: var(--green); }.state.dead { color: var(--red); }.state.warn { color: var(--amber); }
@media (max-width: 1250px) { .health-strip { grid-template-columns: repeat(3, 1fr); }.overview-middle { grid-template-columns: 1fr; }.plugins-body { grid-template-columns: 1fr; } } @media (max-width: 720px) { .health-strip { grid-template-columns: repeat(2, 1fr); }.devices { overflow-x: auto; }.ops-table { min-width: 900px; } }
</style>
