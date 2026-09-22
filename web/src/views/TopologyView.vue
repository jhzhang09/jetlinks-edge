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

	  // 1. 南向插件与南向连接之间（一二列）表示从属关系
  for (const conn of data.value.connections || []) {
    push(`driver-plugin:${conn.driver}`, `connection:${conn.id}`)
  }

  for (const group of data.value.groups) {
    const conn = (data.value.connections || []).find(c => c.id === group.connectionId)
    if (conn) {
	  // 2. 南向连接与采集组之间（二三列）：标识连接健康度状态
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
    <div v-if="error" class="ops-error">{{ error }}</div>

    <div class="topology-shell">
      <div class="flow-container">
        <div class="flow-toolbar">
          <div class="toolbar-left">
            <span class="refresh-badge">{{ t('topo.refresh_interval') }}</span>
          </div>
          <div class="toolbar-legend">
            <!-- 链路图例 -->
            <span class="legend-group">
              <span class="legend-item"><i class="legend-line attr"></i>{{ t('topo.legend_attribution') }}</span>
              <span class="legend-item"><i class="legend-line stream"></i>{{ t('topo.legend_flow') }}</span>
            </span>
            <span class="legend-divider"></span>
            <!-- 状态图例 -->
            <span class="legend-group">
              <b class="good"><span class="status-dot good"></span>{{ t('topo.health') }}</b>
              <b class="warn"><span class="status-dot warn"></span>{{ t('topo.warn') }}</b>
              <b class="bad"><span class="status-dot bad"></span>{{ t('topo.bad') }}</b>
            </span>
          </div>
        </div>

        <section class="flow-board">
          <TopologyFlowCanvas :links="topologyLinks" />

          <!-- 1. 南向插件 (静态归属卡片) -->
          <div class="flow-column col-south-plugin">
            <h3>{{ t('col.south_plugins') }} <small>({{ southPlugins.length }})</small></h3>
            <button
              v-for="plugin in southPlugins"
              :key="plugin.type"
              class="plugin-card"
              :data-topology-node="`driver-plugin:${plugin.type}`"
              @click="select(plugin.type)"
            >
              <div class="card-meta">
                <span class="spec-tag">DRIVER</span>
                <span class="version-tag">v{{ plugin.version }}</span>
              </div>
              <strong>{{ plugin.name }}</strong>
              <small>{{ t('card.channels_count').replace('{count}', String(plugin.count)) }}</small>
            </button>
            <div v-if="!southPlugins.length" class="empty-node">{{ t('empty.south_plugins') }}</div>
          </div>

          <!-- 2. 南向连接 (实时流转卡片) -->
          <div class="flow-column col-channel">
            <h3>{{ t('col.channels') }} <small>({{ (data.connections || []).length }})</small></h3>
            <button
              v-for="conn in sortedConnections"
              :key="conn.id"
              :data-topology-node="`connection:${conn.id}`"
              :class="['runtime-card', 'has-badge', { selected: selectedId === conn.id, failed: conn.enabled && !conn.connected }]"
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

          <!-- 3. 采集组 (实时流转卡片) -->
          <div class="flow-column col-group">
            <h3>{{ t('col.groups') }} <small>({{ data.groups.length }})</small></h3>
            <button
              v-for="group in sortedGroups"
              :key="group.id"
              :data-topology-node="`group:${group.id}`"
              :class="['runtime-card', 'has-badge', { selected: selectedId === group.id, failed: group.enabled && !group.connected }]"
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

          <!-- 4. 北向应用 (实时流转卡片) -->
          <div class="flow-column col-north-app">
            <h3>{{ t('col.north_apps') }} <small>({{ data.northApps.length }})</small></h3>
            <button
              v-for="app in sortedNorthApps"
              :key="app.id"
              :data-topology-node="`north-app:${app.id}`"
              :class="['runtime-card', 'has-badge', { selected: selectedId === app.id, failed: app.enabled && !app.connected }]"
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

          <!-- 5. 北向插件 (静态归属卡片) -->
          <div class="flow-column col-north-plugin">
            <h3>{{ t('col.north_plugins') }} <small>({{ northPlugins.length }})</small></h3>
            <button
              v-for="plugin in northPlugins"
              :key="plugin.type"
              class="plugin-card"
              :data-topology-node="`north-plugin:${plugin.type}`"
            >
              <div class="card-meta">
                <span class="spec-tag">PROTOCOL</span>
                <span class="version-tag">v{{ plugin.version }}</span>
              </div>
              <strong>{{ plugin.name }}</strong>
              <small>{{ t('card.apps_count').replace('{count}', String(plugin.count)) }}</small>
            </button>
            <div v-if="!northPlugins.length" class="empty-node">{{ t('empty.north_plugins') }}</div>
          </div>
        </section>
      </div>

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

.topology-shell {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 220px;
  gap: 12px;
  align-items: start;
}

.flow-container {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 顶栏工具条与双类型图例（对齐拓扑画布宽度，支持自动换行防溢出） */
.flow-toolbar {
  min-height: 38px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  color: var(--muted);
  font-size: 12px;
  padding: 0 2px;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.refresh-badge {
  color: var(--dim);
}
.toolbar-legend {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.legend-group {
  display: flex;
  align-items: center;
  gap: 12px;
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--muted);
  white-space: nowrap;
}
.legend-line {
  display: inline-block;
  width: 18px;
  height: 0;
  vertical-align: middle;
}
.legend-line.attr {
  border-bottom: 1.5px dashed #94a3b8;
  position: relative;
}
.legend-line.attr::after {
  content: '';
  position: absolute;
  right: -2px;
  top: -2px;
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #94a3b8;
}
.legend-line.stream {
  border-bottom: 2px solid var(--cyan);
  position: relative;
}
.legend-line.stream::after {
  content: '';
  position: absolute;
  right: -1px;
  top: -3px;
  border-top: 3px solid transparent;
  border-bottom: 3px solid transparent;
  border-left: 4.5px solid var(--cyan);
}
.legend-divider {
  width: 1px;
  height: 14px;
  background: var(--line);
}
.status-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-right: 4px;
}
.status-dot.good { background: var(--cyan); }
.status-dot.warn { background: var(--amber); }
.status-dot.bad { background: var(--red); }
.flow-toolbar b { font-weight: 500; display: inline-flex; align-items: center; white-space: nowrap; }
.good { color: var(--cyan); }
.warn, .warning { color: var(--amber); }
.bad, .critical { color: var(--red); }

/* 拓扑画布外框与 5 列布局（间距收敛为 36px，卡片更充盈，防屏幕右侧裁切） */
.flow-board {
  min-height: 540px;
  position: relative;
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 36px;
  padding: 16px 14px 20px;
  border: 1px solid var(--line);
  background: var(--surface);
  border-radius: 10px;
  overflow: hidden;
}

.flow-column { position: relative; z-index: 1; min-width: 0; }
.flow-column h3 {
  margin: 0 0 12px;
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.flow-column h3 small { color: var(--dim); font-size: 11px; }

/* 节点通用基类 */
.flow-column button {
  width: 100%;
  height: 86px;
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  margin-bottom: 10px;
  padding: 6px 12px;
  border-radius: 12px;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: all 0.25s cubic-bezier(0.25, 0.8, 0.25, 1);
}
.flow-column button.has-badge {
  padding-top: 4px;
  padding-bottom: 4px;
  gap: 1.5px;
}

/* 1. 插件规范卡片（两头静态归属：精致规格卡片感，无动态流动心跳） */
.plugin-card {
  border: 1px dashed rgba(148, 163, 184, 0.24);
  background: rgba(148, 163, 184, 0.03);
  box-shadow: none;
}
.plugin-card:hover {
  transform: translateY(-2px);
  background: rgba(148, 163, 184, 0.08);
  border-style: solid;
  border-color: rgba(148, 163, 184, 0.45);
}
.plugin-card.selected {
  background: rgba(148, 163, 184, 0.12);
  border-style: solid;
  border-color: rgba(148, 163, 184, 0.6);
}
.col-south-plugin button.plugin-card {
  border-left: 3px dashed #64748b;
}
.col-south-plugin button.plugin-card:hover, .col-south-plugin button.plugin-card.selected {
  border-left-style: solid;
  border-left-color: #94a3b8;
}
.col-north-plugin button.plugin-card {
  border-left: 3px dashed #64748b;
}
.col-north-plugin button.plugin-card:hover, .col-north-plugin button.plugin-card.selected {
  border-left-style: solid;
  border-left-color: #94a3b8;
}

.spec-tag {
  font-size: 8.5px;
  font-weight: 700;
  letter-spacing: 0.6px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(148, 163, 184, 0.16);
  color: #94a3b8;
}
:global(html[data-theme='light']) .spec-tag {
  background: rgba(100, 116, 139, 0.12);
  color: #475569;
}
.version-tag {
  font-size: 10px;
  color: var(--dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

/* 2. 实时链路卡片（中间 3 列：动态运行反馈，彩条与心跳状态点） */
.runtime-card {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.02) 0%, rgba(255, 255, 255, 0.04) 100%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}
.runtime-card:hover {
  transform: translateY(-3px);
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.05) 0%, rgba(255, 255, 255, 0.07) 100%);
}
.runtime-card.selected {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.06) 0%, rgba(255, 255, 255, 0.09) 100%);
  transform: scale(1.01);
}

.col-channel button.runtime-card { border-left: 3px solid #00d2d3; }
.col-channel button.runtime-card:hover, .col-channel button.runtime-card.selected {
  border-color: #00d2d3;
  box-shadow: 0 8px 20px rgba(0, 210, 211, 0.18);
}

.col-group button.runtime-card { border-left: 3px solid #10ac84; }
.col-group button.runtime-card:hover, .col-group button.runtime-card.selected {
  border-color: #10ac84;
  box-shadow: 0 8px 20px rgba(16, 172, 132, 0.18);
}

.col-north-app button.runtime-card { border-left: 3px solid #9b5de5; }
.col-north-app button.runtime-card:hover, .col-north-app button.runtime-card.selected {
  border-color: #9b5de5;
  box-shadow: 0 8px 20px rgba(155, 93, 229, 0.18);
}

.flow-column button.failed {
  border-left-color: var(--red) !important;
  animation: card-pulse-red 2.4s infinite ease-in-out;
}
.flow-column button.failed:hover, .flow-column button.failed.selected {
  border-color: var(--red);
  box-shadow: 0 8px 24px rgba(239, 68, 68, 0.2);
}

/* 胶囊状态 Badge 容器 */
.card-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  margin-bottom: 2px;
}
.runtime-card .card-meta {
  justify-content: flex-end;
}

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
  .flow-board { overflow-x: auto; grid-template-columns: repeat(5, minmax(190px, 1fr)); padding-top: 18px; }
}

:global(html[data-theme='light']) .plugin-card {
  background: rgba(100, 116, 139, 0.03);
  border: 1px dashed rgba(100, 116, 139, 0.25);
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.02);
}
:global(html[data-theme='light']) .plugin-card:hover {
  background: rgba(100, 116, 139, 0.07);
  border-color: rgba(100, 116, 139, 0.45);
}
:global(html[data-theme='light']) .runtime-card {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.01) 0%, rgba(0, 0, 0, 0.02) 100%);
  border: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow: 0 4px 10px rgba(0, 0, 0, 0.02);
}
:global(html[data-theme='light']) .runtime-card:hover {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.02) 0%, rgba(0, 0, 0, 0.04) 100%);
}
:global(html[data-theme='light']) .runtime-card.selected {
  background: linear-gradient(135deg, rgba(0, 0, 0, 0.03) 0%, rgba(0, 0, 0, 0.05) 100%);
}
</style>
