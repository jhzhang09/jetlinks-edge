<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { useThemeStore } from '@/stores/theme'

const props = defineProps<{
  healthy: number[]
  warning: number[]
  critical: number[]
  timestamps?: string[]
}>()

const { t } = useI18n()
const themeStore = useThemeStore()
const isLight = computed(() => themeStore.theme === 'light')
const canvas = ref<HTMLCanvasElement>()
const hoverIndex = ref<number | null>(null)
const mousePos = ref({ x: 0, y: 0 })
let observer: ResizeObserver | undefined

const maxLen = computed(() =>
  Math.max(props.healthy?.length || 0, props.warning?.length || 0, props.critical?.length || 0)
)

const hoverInfo = computed(() => {
  if (hoverIndex.value === null) return null
  const idx = hoverIndex.value
  const len = maxLen.value
  if (idx < 0 || idx >= len) return null

  const time = props.timestamps && props.timestamps[idx]
    ? props.timestamps[idx]
    : len <= 1 ? t('dash.realtime_snapshot') : `Point ${idx + 1}`

  return {
    time,
    healthy: props.healthy[idx] ?? 0,
    warning: props.warning[idx] ?? 0,
    critical: props.critical[idx] ?? 0,
    x: mousePos.value.x,
    y: mousePos.value.y
  }
})

const tooltipStyle = computed(() => {
  if (!hoverInfo.value) return {}
  const chartWidth = canvas.value?.clientWidth || 300
  const isRightHalf = hoverInfo.value.x > chartWidth - 140
  return {
    left: isRightHalf ? `${hoverInfo.value.x - 138}px` : `${hoverInfo.value.x + 14}px`,
    top: `${Math.max(10, Math.min(hoverInfo.value.y - 42, (canvas.value?.clientHeight || 200) - 105))}px`
  }
})

function getCoordinates(width: number, height: number) {
  const left = 38
  const top = 16
  const bottom = height - 26
  const right = width - 14
  return { left, top, bottom, right }
}

function draw() {
  const element = canvas.value
  if (!element) return
  const rect = element.getBoundingClientRect()
  const ratio = window.devicePixelRatio || 1
  element.width = rect.width * ratio
  element.height = rect.height * ratio
  const context = element.getContext('2d')
  if (!context) return
  context.scale(ratio, ratio)

  const width = rect.width
  const height = rect.height
  const { left, top, bottom, right } = getCoordinates(width, height)
  const isLight = themeStore.theme === 'light'

  context.clearRect(0, 0, width, height)

  // 1. Y 轴刻度与横向辅助网格线
  const gridColor = isLight ? 'rgba(0, 0, 0, 0.06)' : 'rgba(255, 255, 255, 0.05)'
  const labelColor = isLight ? '#64748b' : '#94a3b8'
  const axisColor = isLight ? '#cbd5e1' : '#334155'

  context.font = '10.5px SFMono-Regular, Consolas, Menlo, monospace'
  context.textAlign = 'left'
  context.lineWidth = 1

  for (let i = 0; i <= 4; i++) {
    const y = top + ((bottom - top) * i / 4)
    context.strokeStyle = gridColor
    context.beginPath()
    context.moveTo(left, y)
    context.lineTo(right, y)
    context.stroke()

    context.fillStyle = labelColor
    context.fillText(`${100 - i * 25}%`, 2, y + 3.5)
  }

  // 2. X 轴主基线与刻度/时间标签
  context.strokeStyle = axisColor
  context.beginPath()
  context.moveTo(left, bottom)
  context.lineTo(right, bottom)
  context.stroke()

  const len = maxLen.value
  const timestamps = props.timestamps || []

  // 绘制 4~5 个 X 轴时间刻度点
  const tickFractions = [0, 0.25, 0.5, 0.75, 1.0]
  for (const frac of tickFractions) {
    const tickX = left + (right - left) * frac
    // 刻度小短线
    context.strokeStyle = axisColor
    context.beginPath()
    context.moveTo(tickX, bottom)
    context.lineTo(tickX, bottom + 4)
    context.stroke()

    // 刻度时间文本
    let tickLabel = ''
    if (timestamps.length > 0) {
      if (timestamps.length === 1) {
        if (frac === 1) tickLabel = timestamps[0]
      } else {
        const targetIdx = Math.min(timestamps.length - 1, Math.round(frac * (timestamps.length - 1)))
        tickLabel = timestamps[targetIdx]
      }
    } else {
      // 无时间戳时的相对时间回退表示
      const relativeTimes = ['-4m', '-3m', '-2m', '-1m', t('dash.realtime_snapshot')]
      const idx = Math.round(frac * 4)
      tickLabel = relativeTimes[idx]
    }

    if (tickLabel) {
      context.fillStyle = labelColor
      context.textAlign = frac === 0 ? 'left' : frac === 1.0 ? 'right' : 'center'
      context.fillText(tickLabel, tickX, bottom + 16)
    }
  }

  if (len <= 0) return

  // 3. 绘制三条趋势折线
  const series = [
    { values: props.healthy, color: '#31c5e7' },
    { values: props.warning, color: '#f0b54d' },
    { values: props.critical, color: '#f06473' }
  ]

  for (const item of series) {
    if (!item.values || !item.values.length) continue
    context.beginPath()
    item.values.forEach((value, index) => {
      const x = len === 1 ? right : left + ((right - left) * index / (len - 1))
      const y = bottom - ((bottom - top) * Math.max(0, Math.min(value, 100)) / 100)
      if (index === 0) context.moveTo(x, y)
      else context.lineTo(x, y)
    })
    context.strokeStyle = item.color
    context.lineWidth = 2.0
    context.stroke()

    // 最新点端点圆圈
    const lastIndex = item.values.length - 1
    const lastX = len === 1 ? right : left + ((right - left) * lastIndex / (len - 1))
    const lastValue = item.values[lastIndex]
    const lastY = bottom - ((bottom - top) * Math.max(0, Math.min(lastValue, 100)) / 100)

    context.beginPath()
    context.arc(lastX, lastY, 3.5, 0, 2 * Math.PI)
    context.fillStyle = item.color
    context.fill()
  }

  // 4. 鼠标悬停标线（Crosshair Ruler）与高亮数据点指示
  if (hoverIndex.value !== null && hoverIndex.value >= 0 && hoverIndex.value < len) {
    const hIdx = hoverIndex.value
    const hx = len === 1 ? right : left + ((right - left) * hIdx / (len - 1))

    // 垂直辅助标尺虚线
    context.save()
    context.setLineDash([4, 4])
    context.strokeStyle = isLight ? 'rgba(71, 85, 105, 0.4)' : 'rgba(148, 163, 184, 0.45)'
    context.lineWidth = 1.2
    context.beginPath()
    context.moveTo(hx, top)
    context.lineTo(hx, bottom)
    context.stroke()
    context.restore()

    // 各序列对应点的高亮光环（带有内衬白/深色圆圈，即使数据重合也能清晰显现光环）
    for (const item of series) {
      if (!item.values || item.values[hIdx] === undefined) continue
      const val = item.values[hIdx]
      const hy = bottom - ((bottom - top) * Math.max(0, Math.min(val, 100)) / 100)

      context.beginPath()
      context.arc(hx, hy, 4.5, 0, 2 * Math.PI)
      context.fillStyle = isLight ? '#ffffff' : '#0f172a'
      context.strokeStyle = item.color
      context.lineWidth = 2.5
      context.fill()
      context.stroke()
    }
  }
}

function onMouseMove(e: MouseEvent) {
  const element = canvas.value
  if (!element) return
  const rect = element.getBoundingClientRect()
  const x = e.clientX - rect.left
  const y = e.clientY - rect.top
  const { left, right } = getCoordinates(rect.width, rect.height)

  const len = maxLen.value
  if (len <= 0) {
    hoverIndex.value = null
    return
  }

  if (x < left - 12 || x > right + 12) {
    if (hoverIndex.value !== null) {
      hoverIndex.value = null
      draw()
    }
    return
  }

  mousePos.value = { x, y }

  if (len === 1) {
    hoverIndex.value = 0
  } else {
    const clampedX = Math.max(left, Math.min(right, x))
    const ratio = (clampedX - left) / (right - left)
    hoverIndex.value = Math.round(ratio * (len - 1))
  }
  draw()
}

function onMouseLeave() {
  if (hoverIndex.value !== null) {
    hoverIndex.value = null
    draw()
  }
}

watch(
  () => [props.healthy, props.warning, props.critical, props.timestamps, themeStore.theme],
  () => nextTick(draw),
  { deep: true }
)

onMounted(() => {
  observer = new ResizeObserver(draw)
  if (canvas.value) observer.observe(canvas.value)
  draw()
})

onUnmounted(() => observer?.disconnect())
</script>

<template>
  <div class="trend-chart-wrapper" @mousemove="onMouseMove" @mouseleave="onMouseLeave">
    <canvas ref="canvas" class="trend-canvas"></canvas>

    <!-- 鼠标悬停实时多维度数据浮层（严格遵循日夜主题模式） -->
    <div
      v-if="hoverInfo"
      class="trend-tooltip"
      :class="{ 'is-light': isLight }"
      :style="tooltipStyle"
    >
      <div class="tooltip-header">
        <span class="tooltip-time">{{ hoverInfo.time }}</span>
      </div>
      <div class="tooltip-body">
        <div class="tooltip-row">
          <span class="item-dot good"></span>
          <span class="item-label">{{ t('dash.legend_health') }}</span>
          <span class="item-val good">{{ hoverInfo.healthy }}%</span>
        </div>
        <div class="tooltip-row">
          <span class="item-dot warn"></span>
          <span class="item-label">{{ t('dash.legend_warn') }}</span>
          <span class="item-val warn">{{ hoverInfo.warning }}%</span>
        </div>
        <div class="tooltip-row">
          <span class="item-dot bad"></span>
          <span class="item-label">{{ t('dash.legend_bad') }}</span>
          <span class="item-val bad">{{ hoverInfo.critical }}%</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.trend-chart-wrapper {
  position: relative;
  width: 100%;
  height: 100%;
  user-select: none;
}
.trend-canvas {
  width: 100%;
  height: 100%;
  display: block;
  cursor: crosshair;
}

/* 悬停信息提示浮窗 - 默认夜间模式深色玻璃质感 */
.trend-tooltip {
  position: absolute;
  pointer-events: none;
  z-index: 20;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.94);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.14);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  font-size: 11px;
  line-height: 1.4;
  min-width: 130px;
  color: #f1f5f9;
  transition: opacity 0.15s ease-out;
}

/* 白天模式高亮白底质感（支持 .is-light 类与 html[data-theme='light'] 双保险） */
.trend-tooltip.is-light,
:global(html[data-theme='light']) .trend-tooltip {
  background: #ffffff !important;
  border: 1px solid #cbd5e1 !important;
  box-shadow: 0 10px 25px rgba(0, 0, 0, 0.12), 0 2px 6px rgba(0, 0, 0, 0.06) !important;
  color: #0f172a !important;
}

.tooltip-header {
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  padding-bottom: 4px;
  margin-bottom: 6px;
  font-family: var(--mono, monospace);
  font-size: 10.5px;
  color: #94a3b8;
}

.trend-tooltip.is-light .tooltip-header,
:global(html[data-theme='light']) .trend-tooltip .tooltip-header {
  border-bottom: 1px solid #e2e8f0 !important;
  color: #64748b !important;
}

.tooltip-body {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.tooltip-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.item-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.item-label {
  flex-grow: 1;
  color: #94a3b8;
}

.trend-tooltip.is-light .item-label,
:global(html[data-theme='light']) .trend-tooltip .item-label {
  color: #334155 !important;
}

.item-val {
  font-family: var(--mono, monospace);
  font-weight: 600;
}

.item-dot.good, .item-val.good { background: #0284c7; color: #0284c7; }
.item-dot.warn, .item-val.warn { background: #d97706; color: #d97706; }
.item-dot.bad, .item-val.bad { background: #dc2626; color: #dc2626; }
.item-val.good, .item-val.warn, .item-val.bad { background: transparent; }

/* 夜间模式霓虹色彩 */
:not(.is-light) .item-dot.good, :not(.is-light) .item-val.good { background: #31c5e7; color: #31c5e7; }
:not(.is-light) .item-dot.warn, :not(.is-light) .item-val.warn { background: #f0b54d; color: #f0b54d; }
:not(.is-light) .item-dot.bad, :not(.is-light) .item-val.bad { background: #f06473; color: #f06473; }
:not(.is-light) .item-val.good, :not(.is-light) .item-val.warn, :not(.is-light) .item-val.bad { background: transparent; }
</style>
