<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useOperations } from '@/composables/useOperations'
import { useI18n } from '@/i18n'
import TopologyFlowCanvas from '@/components/TopologyFlowCanvas.vue'
import { formatGoTime } from '@/utils/time'

const { t } = useI18n()
const router = useRouter()
const { data, error, refresh } = useOperations(3000)
const selectedId = ref('')
const selectedConnection = computed(() => data.value.connections?.find(item => item.id === selectedId.value))
const selectedGroup = computed(() => data.value.groups.find(item => item.id === selectedId.value))
const selectedNorth = computed(() => data.value.northApps.find(item => item.id === selectedId.value))
const recentEvents = computed(() => [...data.value.alarms].slice(0, 7))

const southPlugins = computed(() => data.value.driverPlugins.map(plugin => ({
  ...plugin,
  count: (data.value.connections || []).filter(conn => conn.driver === plugin.type).length
})))
const northPlugins = computed(() => data.value.northPlugins.map(plugin => ({
  ...plugin,
  count: data.value.northApps.filter(app => app.type === plugin.type).length
})))

const sortedConnections = computed(() => {
  const list = [...(data.value.connections || [])]
  list.sort((a, b) => {
    const idxA = southPlugins.value.findIndex(p => p.type === a.driver)
    const idxB = southPlugins.value.findIndex(p => p.type === b.driver)
    return (idxA === -1 ? 999 : idxA) - (idxB === -1 ? 999 : idxB)
  })
  return list
})

const sortedGroups = computed(() => {
  const list = [...data.value.groups]
  list.sort((a, b) => {
    const idxA = sortedConnections.value.findIndex(c => c.id === a.connectionId)
    const idxB = sortedConnections.value.findIndex(c => c.id === b.connectionId)
    return (idxA === -1 ? 999 : idxA) - (idxB === -1 ? 999 : idxB)
  })
  return list
})

const sortedNorthApps = computed(() => {
  const list = [...data.value.northApps]
  list.sort((a, b) => {
    const idxA = northPlugins.value.findIndex(p => p.type === a.type)
    const idxB = northPlugins.value.findIndex(p => p.type === b.type)
    return (idxA === -1 ? 999 : idxA) - (idxB === -1 ? 999 : idxB)
  })
  return list
})

const topologyLinks = computed(() => {
  const links: { from: string; to: string; status?: 'healthy' | 'warning' | 'critical' }[] = []
  const seen = new Set<string>()
  const push = (from: string, to: string, status?: 'healthy' | 'warning' | 'critical') => {
    const key = `${from}->${to}->${status || 'static'}`
    if (!seen.has(key)) {
      seen.add(key)
      links.push({ from, to, status })
    }
  }

  // 1. 南向插件与物理通道之间 (一二列) 表示从属关系
  for (const conn of data.value.connections || []) {
    push(`driver-plugin:${conn.driver}`, `connection:${conn.id}`)
  }

  for (const group of data.value.groups) {
    const conn = (data.value.connections || []).find(c => c.id === group.connectionId)
    if (conn) {
      // 2. 物理通道与采集组之间 (二三列)：标识连接健康度状态
      const connStatus = !conn.enabled || conn.connected ? 'healthy' : 'critical'
      push(`connection:${conn.id}`, `group:${group.id}`, connStatus)
    }

    if (group.northAppId) {
      const ids = group.northAppId.split(',').filter(Boolean)
      for (const id of ids) {
        const north = data.value.northApps.find(item => item.id === id)
        if (north) {
          // 3. 采集组与北向应用之间 (三四列)：标识连接健康度状态
          const groupStatus = !group.enabled || group.connected ? 'healthy' : 'critical'
          const northStatus = !north.enabled || north.connected ? groupStatus : 'critical'
          push(`group:${group.id}`, `north-app:${north.id}`, northStatus)

          // 4. 北向应用与应用插件之间 (四五列) 表示从属关系
          push(`north-app:${north.id}`, `north-plugin:${north.type}`)
        }
      }
    }
  }
  return links
})
function select(id: string) { selectedId.value = id }
const time = formatGoTime
</script>

<template>
  <div class="ops-page topology-page">
    <div class="ops-heading">
      <div>
        <h1 class="ops-title">{{ t('nav.topology') }}</h1>
        <p class="ops-subtitle">{{ t('topo.relation') }}</p>
      </div>
      <div class="ops-actions">
        <button class="ops-button" @click="refresh">{{ t('topo.refresh') }}</button>
        <button class="ops-button primary" @click="router.push('/groups')">{{ t('topo.config') }}</button>
      </div>
    </div>
    <div class="flow-toolbar"><span>{{ t('topo.refresh_interval') }}</span><div><b class="good">{{ t('topo.health') }}</b><b class="warn">{{ t('topo.warn') }}</b><b class="bad">{{ t('topo.bad') }}</b></div></div>
    <div v-if="error" class="ops-error">{{ error }}</div>

    <div class="topology-shell">
      <section class="flow-board">
        <TopologyFlowCanvas :links="topologyLinks" />
        <div class="flow-column">
          <h3>{{ t('col.south_plugins') }} <small>({{ southPlugins.length }})</small></h3>
          <button
            v-for="plugin in southPlugins"
            :key="plugin.type"
            :data-topology-node="`driver-plugin:${plugin.type}`"
            @click="select(plugin.type)"
          >
            <strong>{{ plugin.name }}</strong>
            <span>{{ plugin.version }}</span>
            <small>{{ t('card.channels_count').replace('{count}', String(plugin.count)) }}</small>
          </button>
          <div v-if="!southPlugins.length" class="empty-node">{{ t('empty.south_plugins') }}</div>
        </div>

        <div class="flow-column">
          <h3>{{ t('col.channels') }} <small>({{ (data.connections || []).length }})</small></h3>
          <button
            v-for="conn in sortedConnections"
            :key="conn.id"
            :data-topology-node="`connection:${conn.id}`"
            :class="['has-badge', { selected: selectedId === conn.id, failed: conn.enabled && !conn.connected }]"
            @click="select(conn.id)"
          >
            <div class="card-meta">
              <div class="node-badge" :class="conn.connected ? 'live' : 'dead'">
                <span class="badge-dot"></span>
                <span class="badge-text">{{ conn.connected ? t('state.connected') : t('state.disconnected') }}</span>
              </div>
            </div>
            <strong>{{ conn.name }}</strong>
            <span>ID: {{ conn.id }}</span>
            <small>{{ conn.driver }}</small>
          </button>
          <div v-if="!(data.connections || []).length" class="empty-node">{{ t('empty.channels') }}</div>
        </div>

        <div class="flow-column">
          <h3>{{ t('col.groups') }} <small>({{ data.groups.length }})</small></h3>
          <button
            v-for="group in sortedGroups"
            :key="group.id"
            :data-topology-node="`group:${group.id}`"
            :class="['has-badge', { selected: selectedId === group.id, failed: group.enabled && !group.connected }]"
            @click="select(group.id)"
          >
            <div class="card-meta">
              <div class="node-badge" :class="group.connected ? 'live' : 'dead'">
                <span class="badge-dot"></span>
                <span class="badge-text">{{ group.connected ? t('state.running') : t('state.stopped') }}</span>
              </div>
            </div>
            <strong>{{ group.name }}</strong>
            <span>ID: {{ group.id }}</span>
            <small>{{ t('card.interval').replace('{ms}', String(group.intervalMs)) }}</small>
          </button>
          <div v-if="!data.groups.length" class="empty-node">{{ t('empty.groups') }}</div>
        </div>

        <div class="flow-column">
          <h3>{{ t('col.north_apps') }} <small>({{ data.northApps.length }})</small></h3>
          <button
            v-for="app in sortedNorthApps"
            :key="app.id"
            :data-topology-node="`north-app:${app.id}`"
            :class="['has-badge', { selected: selectedId === app.id, failed: app.enabled && !app.connected }]"
            @click="select(app.id)"
          >
            <div class="card-meta">
              <div class="node-badge" :class="app.connected ? 'live' : 'dead'">
                <span class="badge-dot"></span>
                <span class="badge-text">{{ app.connected ? t('state.running') : t('state.offline') }}</span>
              </div>
            </div>
            <strong>{{ app.name }}</strong>
            <span>{{ app.type }}</span>
            <small>{{ t('card.groups_bound').replace('{count}', String(data.groups.filter(item => item.northAppId && item.northAppId.split(',').filter(Boolean).includes(app.id)).length)) }}</small>
          </button>
          <div v-if="!data.northApps.length" class="empty-node">{{ t('empty.north_apps') }}</div>
        </div>

        <div class="flow-column">
          <h3>{{ t('col.north_plugins') }} <small>({{ northPlugins.length }})</small></h3>
          <button
            v-for="plugin in northPlugins"
            :key="plugin.type"
            :data-topology-node="`north-plugin:${plugin.type}`"
          >
            <strong>{{ plugin.name }}</strong>
            <span>{{ plugin.version }}</span>
            <small>{{ t('card.apps_count').replace('{count}', String(plugin.count)) }}</small>
          </button>
          <div v-if="!northPlugins.length" class="empty-node">{{ t('empty.north_plugins') }}</div>
        </div>
      </section>

      <aside class="alarm-rail">
        <header>{{ t('log.active_alarms') }} <button @click="router.push('/alarms')">{{ t('log.view_all') }}</button></header>
        <div class="alarm-list">
          <button v-for="alarm in data.alarms.slice(0, 6)" :key="alarm.id" @click="router.push(alarm.route)">
            <b :class="alarm.severity">{{ t('severity.' + alarm.severity) }}</b>
            <strong>{{ alarm.sourceName }}</strong>
            <span>{{ alarm.message }}</span>
            <time>{{ time(alarm.time) }}</time>
          </button>
          <div v-if="!data.alarms.length" class="ops-empty">{{ t('empty.channels') }}</div>
        </div>
        <footer>
          <span>{{ t('log.critical_count').replace('{count}', String(data.alarms.filter(item => item.severity === 'critical').length)) }}</span>
          <span>{{ t('log.warning_count').replace('{count}', String(data.alarms.filter(item => item.severity === 'warning').length)) }}</span>
        </footer>
      </aside>
    </div>

    <section class="ops-panel event-log">
      <header class="ops-panel-header">
        <h2 class="ops-panel-title">{{ t('log.title') }}</h2>
        <span class="ops-panel-meta">{{ t('log.meta') }}</span>
      </header>
      <table class="ops-table">
        <thead>
          <tr>
            <th>{{ t('log.col_time') }}</th>
            <th>{{ t('log.col_level') }}</th>
            <th>{{ t('log.col_source') }}</th>
            <th>{{ t('log.col_message') }}</th>
            <th>{{ t('log.col_component') }}</th>
            <th>{{ t('log.col_target') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="event in recentEvents" :key="event.id">
            <td>{{ time(event.time) }}</td>
            <td :class="event.severity">{{ t('severity.' + event.severity) }}</td>
            <td>{{ t('source.' + event.sourceType) }}</td>
            <td>{{ event.message }}</td>
            <td>{{ t('source.' + event.sourceType) }}</td>
            <td>{{ event.sourceName }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="!recentEvents.length" class="ops-empty">{{ t('log.empty_logs') }}</div>
    </section>
  </div>
</template>

<style scoped>
.topology-tabs { height: 35px; display: flex; align-items: center; gap: 26px; border-bottom: 1px solid var(--line); }
.topology-tabs strong { height: 35px; display: flex; align-items: center; border-bottom: 2px solid var(--cyan); color: var(--text); font-size: 13px; }
.topology-tabs button, .flow-toolbar button, .alarm-rail button { border: 0; background: transparent; color: var(--muted); cursor: pointer; }
.flow-toolbar { min-height: 48px; display: flex; align-items: center; gap: 18px; color: var(--muted); font-size: 12px; }
.flow-toolbar div { margin-left: auto; display: flex; gap: 14px; }
.flow-toolbar b { font-weight: 500; }
.good { color: var(--cyan); }
.warn, .warning { color: var(--amber); }
.bad, .critical { color: var(--red); }

.topology-shell { display: grid; grid-template-columns: minmax(0, 1fr) 220px; gap: 12px; }
.flow-board { min-height: 560px; position: relative; display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 58px; padding: 12px 14px 18px; border: 1px solid var(--line); background: var(--surface); overflow: hidden; }
.flow-column { position: relative; z-index: 1; min-width: 0; }
.flow-column h3 { margin: 0 0 12px; color: var(--text); font-size: 13px; }
.flow-column h3 small { color: var(--dim); }
.flow-column button {
  width: 100%;
  height: 86px;
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  margin-bottom: 10px;
  padding: 6px 14px;
  border: 1px solid rgba(255, 255, 255, 0.05);
  border-radius: 12px;
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.02) 0%, rgba(255, 255, 255, 0.04) 100%);
  color: inherit;
  text-align: left;
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  transition: all 0.3s cubic-bezier(0.25, 0.8, 0.25, 1);
}
.flow-column button.has-badge {
  padding-top: 4px;
  padding-bottom: 4px;
  gap: 1.5px;
}
.flow-column button:hover {
  transform: translateY(-3px);
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.05) 0%, rgba(255, 255, 255, 0.07) 100%);
}
.flow-column button.selected {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.06) 0%, rgba(255, 255, 255, 0.09) 100%);
  transform: scale(1.01);
}

/* 5 列指示彩条及悬浮发光配色 */
.flow-column:nth-of-type(1) button { border-left: 3px solid #ff9f43; }
.flow-column:nth-of-type(1) button:hover, .flow-column:nth-of-type(1) button.selected { border-color: #ff9f43; box-shadow: 0 8px 20px rgba(255, 159, 67, 0.15); }

.flow-column:nth-of-type(2) button { border-left: 3px solid #00d2d3; }
.flow-column:nth-of-type(2) button:hover, .flow-column:nth-of-type(2) button.selected { border-color: #00d2d3; box-shadow: 0 8px 20px rgba(0, 210, 211, 0.15); }

.flow-column:nth-of-type(3) button { border-left: 3px solid #10ac84; }
.flow-column:nth-of-type(3) button:hover, .flow-column:nth-of-type(3) button.selected { border-color: #10ac84; box-shadow: 0 8px 20px rgba(16, 172, 132, 0.15); }

.flow-column:nth-of-type(4) button { border-left: 3px solid #9b5de5; }
.flow-column:nth-of-type(4) button:hover, .flow-column:nth-of-type(4) button.selected { border-color: #9b5de5; box-shadow: 0 8px 20px rgba(155, 93, 229, 0.15); }

.flow-column:nth-of-type(5) button { border-left: 3px solid #f15bb5; }
.flow-column:nth-of-type(5) button:hover, .flow-column:nth-of-type(5) button.selected { border-color: #f15bb5; box-shadow: 0 8px 20px rgba(241, 91, 181, 0.15); }

.flow-column button.failed {
  border-left-color: var(--red) !important;
  animation: card-pulse-red 2.4s infinite ease-in-out;
}
.flow-column button.failed:hover, .flow-column button.failed.selected {
  border-color: var(--red);
  box-shadow: 0 8px 24px rgba(239, 68, 68, 0.2);
}

/* 胶囊状态 Badge 容器 */
.node-badge {
  display: flex;
  align-items: center;
  gap: 3.5px;
  padding: 1.5px 5px;
  border-radius: 9px;
  font-size: 8.5px;
  font-weight: 600;
  line-height: 1;
  border: 1px solid rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(4px);
}
.node-badge.live {
  background: rgba(16, 185, 129, 0.08);
  color: var(--green);
  border-color: rgba(16, 185, 129, 0.22);
}
.node-badge.live .badge-dot {
  position: relative;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--green);
  box-shadow: 0 0 5px var(--green);
}
.node-badge.live .badge-dot::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: var(--green);
  box-shadow: 0 0 5px var(--green);
  animation: dot-ping 1.6s cubic-bezier(0, 0, 0.2, 1) infinite;
}
.node-badge.dead {
  background: rgba(239, 68, 68, 0.08);
  color: var(--red);
  border-color: rgba(239, 68, 68, 0.22);
}
.node-badge.dead .badge-dot {
  position: relative;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--red);
  box-shadow: 0 0 5px var(--red);
}
.node-badge.dead .badge-dot::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: var(--red);
  box-shadow: 0 0 5px var(--red);
  animation: dot-ping 1.2s cubic-bezier(0, 0, 0.2, 1) infinite;
}

@keyframes dot-ping {
  0% {
    transform: scale(1);
    opacity: 0.85;
  }
  100% {
    transform: scale(2.6);
    opacity: 0;
  }
}
@keyframes card-pulse-red {
  0%, 100% {
    box-shadow: 0 2px 6px rgba(239, 68, 68, 0.04);
    border-color: rgba(239, 68, 68, 0.6);
  }
  50% {
    box-shadow: 0 4px 12px rgba(239, 68, 68, 0.14);
    border-color: rgba(239, 68, 68, 1);
  }
}
.card-meta {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  width: 100%;
  margin-bottom: 2px;
}
.flow-column strong {
  color: var(--text);
  font-size: 13.5px;
  font-weight: 600;
  width: 100%;
  display: block;
  box-sizing: border-box;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-bottom: 1px;
}
.flow-column span, .flow-column small { color: var(--muted); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; opacity: 0.85; }
.empty-node { min-height: 76px; display: grid; place-items: center; border: 1px dashed var(--line); color: var(--dim); font-size: 12px; }

.alarm-rail {
  border: 1px solid var(--line);
  background: var(--surface);
  height: 560px;
  display: flex;
  flex-direction: column;
}
.alarm-rail header { height: 46px; display: flex; justify-content: space-between; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--line); color: var(--text); font-size: 13px; }
.alarm-rail header button { color: var(--cyan); font-size: 12px; }
.alarm-rail .alarm-list { flex: 1; overflow-y: auto; }
.alarm-rail .alarm-list::-webkit-scrollbar { width: 4px; }
.alarm-rail .alarm-list::-webkit-scrollbar-thumb { background: var(--line-strong); border-radius: 2px; }
.alarm-rail .alarm-list > button { width: 100%; min-height: 86px; display: flex; flex-direction: column; gap: 6px; padding: 12px; border: 0; border-bottom: 1px solid var(--line); background: transparent; text-align: left; cursor: pointer; }
.alarm-rail .alarm-list > button:hover { background: var(--surface-3); }
.alarm-rail b { color: var(--amber); font-size: 12px; }
.alarm-rail b.critical { color: var(--red); }
.alarm-rail strong { color: var(--text); font-size: 13px; }
.alarm-rail span, .alarm-rail time { color: var(--muted); font-size: 12px; }
.alarm-rail footer { display: flex; justify-content: space-around; padding: 12px 4px; border-top: 1px solid var(--line); color: var(--muted); font-size: 12px; }

.event-log { margin-top: 10px; }
.event-log td.critical { color: var(--red); }
.event-log td.warning { color: var(--amber); }

@media (max-width: 1150px) {
  .topology-shell { grid-template-columns: 1fr; }
  .alarm-rail { display: none; }
  .flow-board { overflow-x: auto; grid-template-columns: repeat(5, minmax(190px, 1fr)); }
}

:global(html[data-theme='light']) .flow-column button {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.01) 0%, rgba(0, 0, 0, 0.02) 100%);
  border: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow: 0 4px 10px rgba(0, 0, 0, 0.02);
}
:global(html[data-theme='light']) .flow-column button:hover {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.02) 0%, rgba(0, 0, 0, 0.04) 100%);
}
:global(html[data-theme='light']) .flow-column button.selected {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.03) 0%, rgba(0, 0, 0, 0.05) 100%);
}
</style>
