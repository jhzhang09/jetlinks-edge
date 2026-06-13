import { computed, onMounted, onUnmounted, ref } from 'vue'
import { operations, reloadGroup, reloadNorthApp, type OperationsView } from '@/api'

const emptyView: OperationsView = {
  generatedAt: '',
  startTime: '',
  runtime: {
    nodeId: 'jetlinks-edge',
    goroutines: 0,
    memoryAllocBytes: 0,
    memorySysBytes: 0,
    memoryUsedPercent: 0,
    uptimeSeconds: 0
  },
  connections: [],
  groups: [],
  northApps: [],
  driverPlugins: [],
  northPlugins: [],
  alarms: [],
  recentValues: []
}

// 模块级全局共享状态，避免切换页面或多实例重挂载时历史记录丢失
const globalData = ref<OperationsView>(emptyView)
const globalLoading = ref(false)
const globalError = ref('')
const globalHistory = ref<{ healthy: number[]; warning: number[]; critical: number[] }>({ healthy: [], warning: [], critical: [] })

// 活跃实例计数器，控制仅由一个全局定时器执行网络轮询
let activeInstances = 0
let globalTimer: number | undefined

export function useOperations(refreshInterval = 5000) {
  const data = globalData
  const loading = globalLoading
  const error = globalError
  const history = globalHistory

  const connectedConnections = computed(() => data.value.connections.filter(item => item.connected).length)
  const enabledConnections = computed(() => data.value.connections.filter(item => item.enabled).length)
  const connectedGroups = computed(() => data.value.groups.filter(item => item.connected).length)
  const enabledGroups = computed(() => data.value.groups.filter(item => item.enabled).length)
  const connectedNorth = computed(() => data.value.northApps.filter(item => item.connected).length)
  const enabledNorth = computed(() => data.value.northApps.filter(item => item.enabled).length)
  const criticalAlarms = computed(() => data.value.alarms.filter(item => item.severity === 'critical').length)

  // 科学评估网关健康度评分（基于组件状态分类，北向应用只跟北向告警关联，南向设备只跟南向及点位告警关联）
  const overallHealth = computed(() => {
    const total = data.value.groups.filter(g => g.enabled).length + data.value.northApps.filter(n => n.enabled).length
    if (!total) return 100
    
    let healthyCount = 0
    
    // 1. 判定北向应用是否完全健康
    for (const app of data.value.northApps.filter(n => n.enabled)) {
      if (!app.connected) continue
      const hasAlarms = data.value.alarms.some(a => a.sourceType === 'north' && a.sourceId === app.id)
      if (!hasAlarms) healthyCount++
    }
    
    // 2. 判定南向设备是否完全健康
    for (const group of data.value.groups.filter(g => g.enabled)) {
      if (!group.connected) continue
      const hasAlarms = data.value.alarms.some(a => {
        if (a.sourceType === 'group' && a.sourceId === group.id) return true
        if (a.sourceType === 'tag' && (a.sourceName.startsWith(group.name) || a.route.includes('/groups/' + group.id))) return true
        return false
      })
      if (!hasAlarms) healthyCount++
    }
    
    return Math.round(healthyCount * 100 / total)
  })

  async function refresh() {
    loading.value = true
    try {
      const res = await operations()
      data.value = res
      
      const total = res.groups.filter(g => g.enabled).length + res.northApps.filter(n => n.enabled).length
      
      if (total === 0) {
        history.value.healthy.push(100)
        history.value.warning.push(0)
        history.value.critical.push(0)
      } else {
        let healthyCount = 0
        let warningCount = 0
        let criticalCount = 0
        
        // 1. 北向应用状态分类
        for (const app of res.northApps.filter(n => n.enabled)) {
          const hasCritical = !app.connected || res.alarms.some(a => a.sourceType === 'north' && a.sourceId === app.id && a.severity === 'critical')
          const hasWarning = app.connected && res.alarms.some(a => a.sourceType === 'north' && a.sourceId === app.id && a.severity === 'warning')
          
          if (hasCritical) {
            criticalCount++
          } else if (hasWarning) {
            warningCount++
          } else {
            healthyCount++
          }
        }
        
        // 2. 南向设备与下辖点位状态分类
        for (const group of res.groups.filter(g => g.enabled)) {
          const isGroupAlarm = (a: any) => {
            if (a.sourceType === 'group' && a.sourceId === group.id) return true
            if (a.sourceType === 'tag' && (a.sourceName.startsWith(group.name) || a.route.includes('/groups/' + group.id))) return true
            return false
          }
          const hasCritical = !group.connected || res.alarms.some(a => isGroupAlarm(a) && a.severity === 'critical')
          const hasWarning = group.connected && res.alarms.some(a => isGroupAlarm(a) && a.severity === 'warning')
          
          if (hasCritical) {
            criticalCount++
          } else if (hasWarning) {
            warningCount++
          } else {
            healthyCount++
          }
        }
        
        history.value.healthy.push(Math.round(healthyCount * 100 / total))
        history.value.warning.push(Math.round(warningCount * 100 / total))
        history.value.critical.push(Math.round(criticalCount * 100 / total))
      }
      
      for (const values of Object.values(history.value)) {
        if (values.length > 48) values.splice(0, values.length - 48)
      }
      error.value = ''
    } catch (cause: any) {
      error.value = cause?.message || '加载运维状态失败'
    } finally {
      loading.value = false
    }
  }

  async function recover(sourceType: string, sourceId: string) {
    if (sourceType === 'group' || sourceType === 'tag') await reloadGroup(sourceId)
    if (sourceType === 'north') await reloadNorthApp(sourceId)
    await refresh()
  }

  onMounted(() => {
    activeInstances++
    if (activeInstances === 1) {
      refresh()
      globalTimer = window.setInterval(refresh, refreshInterval)
    }
  })

  onUnmounted(() => {
    activeInstances--
    if (activeInstances <= 0) {
      if (globalTimer) {
        window.clearInterval(globalTimer)
        globalTimer = undefined
      }
    }
  })

  return {
    data,
    loading,
    error,
    connectedConnections,
    enabledConnections,
    connectedGroups,
    enabledGroups,
    connectedNorth,
    enabledNorth,
    criticalAlarms,
    overallHealth,
    history,
    refresh,
    recover
  }
}
