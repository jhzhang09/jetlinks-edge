<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useOperations } from '@/composables/useOperations'
import { useI18n } from '@/i18n'
import OpsTrendChart from '@/components/OpsTrendChart.vue'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const { data, error, recover, refresh, history } = useOperations()
const selectedId = ref('')
const severity = ref<'all' | 'critical' | 'warning'>('all')
const recovering = ref(false)
const alarms = computed(() => severity.value === 'all' ? data.value.alarms : data.value.alarms.filter(item => item.severity === severity.value))
const selected = computed(() => data.value.alarms.find(item => item.id === selectedId.value) || alarms.value[0])
const relatedValues = computed(() => selected.value ? data.value.recentValues.filter(item => item.groupId === selected.value?.sourceId).slice(0, 8) : [])
const suggestions = computed(() => selected.value?.sourceType === 'north' 
  ? [t('alarms.suggest.north_net'), t('alarms.suggest.north_auth'), t('alarms.suggest.north_restart')] 
  : [t('alarms.suggest.south_net'), t('alarms.suggest.south_port'), t('alarms.suggest.south_reload')])
watch(alarms, items => { if (!selectedId.value && items.length) selectedId.value = items[0].id }, { immediate: true })
function time(value?: string) { return !value || value.startsWith('0001-') ? '--' : new Date(value).toLocaleString() }
async function retry() {
  if (!selected.value) return
  recovering.value = true
  try { await recover(selected.value.sourceType, selected.value.sourceId); message.success(t('alarms.recheck_success')) }
  catch (cause: any) { message.error(cause?.message || t('common.reload_failed')) }
  finally { recovering.value = false }
}
</script>

<template>
  <div class="ops-page alarm-page">
    <div v-if="error" class="ops-error">{{ error }}</div>
    <div class="alarm-layout">
      <section class="queue-column">
        <header><h1>{{ t('alarms.queue') }} <small>({{ data.alarms.length }})</small></h1><button @click="refresh">{{ t('devices.refresh') }}</button></header>
        <div class="severity-tabs"><button :class="{ active: severity === 'all' }" @click="severity = 'all'">{{ t('common.all') }} {{ data.alarms.length }}</button><button :class="{ active: severity === 'critical' }" @click="severity = 'critical'">{{ t('severity.critical') }} {{ data.alarms.filter(item => item.severity === 'critical').length }}</button><button :class="{ active: severity === 'warning' }" @click="severity = 'warning'">{{ t('severity.warning') }} {{ data.alarms.filter(item => item.severity === 'warning').length }}</button></div>
        <button v-for="alarm in alarms" :key="alarm.id" class="alarm-card" :class="{ selected: selected?.id === alarm.id, critical: alarm.severity === 'critical' }" @click="selectedId = alarm.id"><span><time>{{ time(alarm.time) }}</time><b>{{ t('severity.' + alarm.severity) }}</b></span><strong>{{ alarm.sourceName }} {{ alarm.message }}</strong><small>{{ t('source.' + alarm.sourceType) }} / {{ alarm.sourceId }}</small><em>{{ t('alarms.unresolved') }}</em></button>
        <div v-if="!alarms.length" class="ops-empty">{{ t('dash.no_active_alarms') }}</div>
      </section>

      <main class="diagnosis-column">
        <h1>{{ t('alarms.details_diagnosis') }}</h1>
        <template v-if="selected">
          <section class="incident-summary"><div><b>{{ t('severity.' + selected.severity) }}</b><h2>{{ selected.sourceName }} {{ selected.message }}</h2><p>{{ t('source.' + selected.sourceType) }} / {{ selected.sourceId }}</p></div><dl><dt>{{ t('alarms.first_time') }}</dt><dd>{{ time(selected.time) }}</dd><dt>{{ t('alarms.status') }}</dt><dd>{{ t('alarms.unresolved') }}</dd><dt>{{ t('alarms.id') }}</dt><dd>{{ selected.id }}</dd></dl></section>
          <div class="diagnosis-tabs"><strong>{{ t('alarms.evidence_timeline') }}</strong><span>{{ t('alarms.metrics') }}</span><span>{{ t('alarms.device_info') }}</span></div>
          <div class="diagnosis-grid">
            <section class="timeline ops-panel"><header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('alarms.diagnosis_overview') }}</h2></header><div class="timeline-list"><div><b class="bad"></b><span>{{ time(selected.time) }}</span><strong>{{ t('alarms.detect_fault') }}</strong><small>{{ selected.message }}</small></div><div><b class="warn"></b><span>{{ t('alarms.current') }}</span><strong>{{ t('alarms.wait_action') }}</strong><small>{{ t('alarms.fault_exists') }}</small></div><div><b class="good"></b><span>{{ t('alarms.after_ops') }}</span><strong>{{ t('alarms.auto_recheck') }}</strong><small>{{ t('alarms.clear_desc') }}</small></div></div></section>
            <section class="evidence ops-panel"><header class="ops-panel-header"><h2 class="ops-panel-title">{{ t('alarms.trend') }}</h2><span class="ops-panel-meta">{{ t('dash.realtime_snapshot') }}</span></header><div class="chart"><OpsTrendChart :healthy="history.healthy" :warning="history.warning" :critical="history.critical" /></div><h3>{{ t('alarms.recent_tags') }}</h3><table class="ops-table"><thead><tr><th>{{ t('alarms.col_tag_name') }}</th><th>{{ t('alarms.col_live_val') }}</th><th>{{ t('alarms.col_quality') }}</th><th>{{ t('tags.col_update_time') }}</th></tr></thead><tbody><tr v-for="item in relatedValues" :key="item.tagId"><td>{{ item.name }}</td><td>{{ item.value }}</td><td :class="item.quality === 'good' ? 'good' : 'bad'">{{ t(item.quality === 'good' ? 'tags.quality_good' : 'tags.quality_bad') }}</td><td>{{ time(item.time) }}</td></tr></tbody></table><div v-if="!relatedValues.length" class="small-empty">{{ t('alarms.empty_recent') }}</div></section>
          </div>
        </template>
        <div v-else class="ops-empty full-empty">{{ t('alarms.system_ok') }}</div>
      </main>

      <aside class="recovery-column">
        <h1>{{ t('alarms.details_diagnosis') }}</h1>
        <section><h2>{{ t('alarms.rca') }}</h2><p v-if="selected">{{ t('alarms.evidence_desc').replace('{msg}', selected.message) }}</p><ul><li v-for="item in suggestions" :key="item">{{ item }}</li></ul></section>
        <section><h2>{{ t('alarms.recommend') }}</h2><ol><li v-for="(item, index) in suggestions" :key="item"><b>{{ index + 1 }}</b>{{ item }}</li></ol></section>
        <section class="quick-actions"><h2>{{ t('alarms.quick_ops') }}</h2><button :disabled="!selected || recovering" @click="retry">{{ recovering ? t('alarms.rechecking') : t('alarms.recheck') }}</button><button :disabled="!selected" @click="selected && router.push(selected.route)">{{ t('alarms.open_detail') }}</button><button @click="router.push('/topology')">{{ t('alarms.view_topo') }}</button></section>
        <section><h2>{{ t('alarms.records') }}</h2><textarea :placeholder="t('alarms.record_placeholder')"></textarea><button class="record">{{ t('alarms.record') }}</button></section>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.alarm-page { margin: -18px -20px -28px; width: auto; }
.alarm-layout { min-height: calc(100vh - 66px); display: grid; grid-template-columns: 360px minmax(560px, 1fr) 320px; }
.queue-column, .diagnosis-column, .recovery-column { min-width: 0; border-right: 1px solid var(--line); background: var(--surface); }
.recovery-column { border: 0; }
.queue-column > header, .diagnosis-column > h1, .recovery-column > h1 { height: 56px; display: flex; align-items: center; justify-content: space-between; margin: 0; padding: 0 16px; border-bottom: 1px solid var(--line); color: var(--text); font-size: 16px; }
.queue-column header small { color: var(--muted); }
.queue-column header button { min-height: 30px; border: 1px solid var(--line); background: var(--surface-2); color: var(--muted); font-size: 12px; }
.severity-tabs { height: 46px; display: grid; grid-template-columns: repeat(3, 1fr); gap: 4px; padding: 6px 10px; border-bottom: 1px solid var(--line); }
.severity-tabs button { border: 0; background: transparent; color: var(--muted); font-size: 12px; cursor: pointer; }
.severity-tabs button.active { color: var(--cyan); background: var(--surface-3); }
.alarm-card { width: 100%; min-height: 126px; display: flex; flex-direction: column; gap: 8px; padding: 14px 16px; border: 0; border-bottom: 1px solid var(--line); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.alarm-card:hover, .alarm-card.selected { background: var(--surface-3); box-shadow: inset 2px 0 var(--amber); }
.alarm-card.selected.critical { box-shadow: inset 2px 0 var(--red); }
.alarm-card span { display: flex; gap: 10px; }
.alarm-card time, .alarm-card small { color: var(--muted); font-size: 12px; }
.alarm-card b { color: var(--amber); font-size: 12px; }
.alarm-card.critical b { color: var(--red); }
.alarm-card strong { color: var(--text); font-size: 14px; }
.alarm-card em { align-self: flex-start; padding: 3px 8px; background: var(--surface-2); color: var(--muted); font-size: 12px; font-style: normal; }
.incident-summary { margin: 16px; padding: 18px; border: 1px solid var(--line); }
.incident-summary > div { border-bottom: 1px solid var(--line); padding-bottom: 14px; }
.incident-summary b { color: var(--red); font-size: 13px; }
.incident-summary h2 { display: inline; margin-left: 10px; color: var(--text); font-size: 18px; }
.incident-summary p { margin: 8px 0 0; color: var(--muted); font-size: 13px; }
.incident-summary dl { display: grid; grid-template-columns: repeat(3, 1fr); margin: 14px 0 0; }
.incident-summary dt { color: var(--dim); font-size: 12px; }
.incident-summary dd { margin: 6px 0 0; color: var(--muted); font-size: 13px; }
.diagnosis-tabs { height: 42px; display: flex; gap: 26px; align-items: center; padding: 0 16px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 13px; }
.diagnosis-tabs strong { height: 42px; display: flex; align-items: center; border-bottom: 2px solid var(--cyan); color: var(--cyan); }
.diagnosis-grid { display: grid; grid-template-columns: 270px minmax(0, 1fr); gap: 12px; padding: 12px 16px; }
.timeline-list { padding: 14px; }
.timeline-list div { min-height: 96px; position: relative; padding-left: 20px; border-left: 1px solid var(--line-strong); }
.timeline-list b { position: absolute; left: -4px; top: 1px; width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.timeline-list span, .timeline-list strong, .timeline-list small { display: block; }
.timeline-list span { color: var(--muted); font-size: 12px; }
.timeline-list strong { margin: 7px 0 5px; color: var(--text); font-size: 13px; }
.timeline-list small { color: var(--dim); font-size: 12px; }
.chart { height: 220px; padding: 12px; }
.evidence h3 { margin: 0; padding: 12px; border-top: 1px solid var(--line); color: var(--muted); font-size: 13px; }
.small-empty { padding: 20px; color: var(--dim); font-size: 13px; text-align: center; }
.full-empty { min-height: 500px; }
.recovery-column section { margin: 12px; padding: 14px; border: 1px solid var(--line); }
.recovery-column h2 { margin: 0 0 12px; color: var(--text); font-size: 14px; }
.recovery-column p, .recovery-column li { color: var(--muted); font-size: 12px; line-height: 1.7; }
.recovery-column ul, .recovery-column ol { padding-left: 16px; }
.recovery-column ol { list-style: none; padding: 0; }
.recovery-column ol li { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.recovery-column ol b { width: 20px; height: 20px; display: grid; place-items: center; border-radius: 50%; background: var(--surface-3); color: var(--muted); }
.quick-actions button, .record { width: 100%; min-height: 40px; margin-bottom: 8px; border: 1px solid var(--line); background: var(--surface-2); color: var(--text); font-size: 13px; cursor: pointer; }
.quick-actions button:first-of-type, .record { border-color: #1f6575; color: var(--cyan); }
.recovery-column textarea { width: 100%; min-height: 80px; padding: 8px; border: 1px solid var(--line); background: var(--surface-2); color: var(--text); font-size: 13px; resize: vertical; }

@media (max-width: 1200px) {
  .alarm-layout { grid-template-columns: 280px 1fr; }
  .recovery-column { display: none; }
}
@media (max-width: 800px) {
  .alarm-page { margin: 0; }
  .alarm-layout { grid-template-columns: 1fr; }
  .queue-column { max-height: 380px; overflow-y: auto; }
  .diagnosis-grid { grid-template-columns: 1fr; }
}
</style>
