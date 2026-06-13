<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useThemeStore } from '@/stores/theme'

type TopologyLink = {
  from: string
  to: string
  status?: 'healthy' | 'warning' | 'critical'
}

const props = defineProps<{ links: TopologyLink[] }>()
const themeStore = useThemeStore()
const canvas = ref<HTMLCanvasElement>()
let observer: ResizeObserver | undefined
let frame = 0

function getCssColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || '#31c5e7'
}

function nodeRect(board: DOMRect, id: string) {
  const node = canvas.value
    ?.parentElement
    ?.querySelector<HTMLElement>(`[data-topology-node="${CSS.escape(id)}"]`)
  if (!node) return undefined
  const rect = node.getBoundingClientRect()
  return {
    left: rect.left - board.left,
    right: rect.right - board.left,
    top: rect.top - board.top,
    bottom: rect.bottom - board.top,
    y: rect.top - board.top + rect.height / 2
  }
}

function scheduleDraw() {
  // 仅在有 Resize 事件发生时重新调整大小或触发重绘（在持续动画循环中其实每帧都在画）
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
  context.clearRect(0, 0, rect.width, rect.height)

  const board = element.parentElement?.getBoundingClientRect()
  if (!board) return

  // 根据当前毫秒时间计算流光位移量和呼吸闪烁不透明度值
  const now = Date.now()
  const flowOffset = (now / 35) % 20
  const pulseOpacity = 0.35 + 0.65 * Math.sin(now / 150) // 警告快速呼吸闪烁

  for (const link of props.links) {
    const from = nodeRect(board, link.from)
    const to = nodeRect(board, link.to)
    if (!from || !to) continue

    const isLight = themeStore.theme === 'light'
    const color = link.status === 'critical'
      ? getCssColor('--red')
      : link.status === 'warning'
        ? getCssColor('--amber')
        : link.status === 'healthy'
          ? getCssColor('--cyan')
          : (isLight ? '#475569' : '#94a3b8') // 静态线颜色明显反转（Light用深灰对比，Dark用浅灰对比）

    const startX = from.right
    const endX = to.left
    const gap = Math.max(28, Math.min(90, (endX - startX) * .42))

    context.strokeStyle = color
    context.shadowColor = color

    if (!link.status) {
      // 1. 静态从属连线：加粗为 2.0px，对比清晰，无阴影
      context.lineWidth = 2.0
      context.shadowBlur = 0
      context.globalAlpha = isLight ? 0.9 : 0.82
      context.beginPath()
      context.moveTo(startX, from.y)
      context.bezierCurveTo(startX + gap, from.y, endX - gap, to.y, endX, to.y)
      context.stroke()
    } else if (link.status === 'healthy') {
      // 2. 健康传输通道：动态向右流光效果 (双层叠加：一层半透明实线底色 + 一层流动虚线)
      // 底色细实线
      context.lineWidth = 2.0
      context.shadowBlur = isLight ? 0 : 4
      context.globalAlpha = isLight ? 0.45 : 0.35
      context.beginPath()
      context.moveTo(startX, from.y)
      context.bezierCurveTo(startX + gap, from.y, endX - gap, to.y, endX, to.y)
      context.stroke()

      // 流动虚线
      context.lineWidth = 3.2 // 明显加粗流光
      context.shadowBlur = isLight ? 0 : 8
      context.globalAlpha = isLight ? 0.95 : 0.88
      context.setLineDash([7, 6])
      context.lineDashOffset = -flowOffset // 负数实现向右流动
      context.stroke()
      context.setLineDash([]) // 恢复实线
    } else {
      // 3. 故障/警告传输通道：动态高频呼吸闪烁警示
      context.lineWidth = 3.5 // 明显加粗
      context.shadowBlur = isLight ? 0 : 10
      context.globalAlpha = Math.max(0.35, pulseOpacity)
      context.beginPath()
      context.moveTo(startX, from.y)
      context.bezierCurveTo(startX + gap, from.y, endX - gap, to.y, endX, to.y)
      context.stroke()
    }

    // 绘制端部箭头 (颜色和透明度与连线同步)
    context.globalAlpha = !link.status 
      ? (isLight ? 0.9 : 0.82) 
      : (link.status === 'healthy' ? (isLight ? 0.95 : 0.88) : Math.max(0.35, pulseOpacity))
    context.shadowBlur = 0
    context.fillStyle = color
    context.beginPath()
    context.moveTo(endX, to.y)
    context.lineTo(endX - 9, to.y - 5.5)
    context.lineTo(endX - 9, to.y + 5.5)
    context.closePath()
    context.fill()
  }
}

// 动画循环渲染
function animate() {
  draw()
  frame = window.requestAnimationFrame(animate)
}

watch(() => props.links, scheduleDraw, { deep: true })
watch(() => themeStore.theme, scheduleDraw)

onMounted(() => {
  observer = new ResizeObserver(scheduleDraw)
  if (canvas.value?.parentElement) observer.observe(canvas.value.parentElement)
  animate() // 挂载时激活 60FPS 重绘动画
})

onUnmounted(() => {
  window.cancelAnimationFrame(frame)
  observer?.disconnect()
})
</script>

<template><canvas ref="canvas"></canvas></template>

<style scoped>
canvas { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; opacity: .55; }
:global(html[data-theme='light']) canvas { opacity: .82; }
</style>
