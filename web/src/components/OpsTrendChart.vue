<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  healthy: number[]
  warning: number[]
  critical: number[]
}>()

const canvas = ref<HTMLCanvasElement>()
let observer: ResizeObserver | undefined

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
  const left = 34
  const top = 12
  const bottom = height - 22
  const right = width - 12
  context.clearRect(0, 0, width, height)
  context.font = '11px SFMono-Regular, Consolas, Menlo, monospace'
  context.fillStyle = '#455965'
  context.strokeStyle = '#172632'
  context.lineWidth = 1
  for (let index = 0; index <= 4; index++) {
    const y = top + ((bottom - top) * index / 4)
    context.beginPath()
    context.moveTo(left, y)
    context.lineTo(right, y)
    context.stroke()
    context.fillText(`${100 - index * 25}%`, 2, y + 3)
  }
  const series = [
    { values: props.healthy, color: '#31c5e7' },
    { values: props.warning, color: '#f0b54d' },
    { values: props.critical, color: '#f06473' }
  ]
  const totalPoints = 48
  for (const item of series) {
    if (!item.values.length) continue
    context.beginPath()
    item.values.forEach((value, index) => {
      // 计算每个点在 48 格网格中的绝对索引，使未满的数据靠右对齐绘制
      const pointIndex = index + (totalPoints - item.values.length)
      const x = left + ((right - left) * pointIndex / (totalPoints - 1))
      const y = bottom - ((bottom - top) * Math.max(0, Math.min(value, 100)) / 100)
      if (index === 0) context.moveTo(x, y)
      else context.lineTo(x, y)
    })
    context.strokeStyle = item.color
    context.lineWidth = 1.5
    context.stroke()

    // 最新点高亮圆端点渲染（特别解决只有一个点时线画不出来的问题）
    if (item.values.length > 0) {
      const lastIndex = item.values.length - 1
      const pointIndex = lastIndex + (totalPoints - item.values.length)
      const x = left + ((right - left) * pointIndex / (totalPoints - 1))
      const lastValue = item.values[lastIndex]
      const y = bottom - ((bottom - top) * Math.max(0, Math.min(lastValue, 100)) / 100)
      
      context.beginPath()
      context.arc(x, y, 3, 0, 2 * Math.PI)
      context.fillStyle = item.color
      context.fill()
    }
  }
}

watch(() => [props.healthy, props.warning, props.critical], () => nextTick(draw), { deep: true })
onMounted(() => {
  observer = new ResizeObserver(draw)
  if (canvas.value) observer.observe(canvas.value)
  draw()
})
onUnmounted(() => observer?.disconnect())
</script>

<template><canvas ref="canvas" class="trend-canvas"></canvas></template>

<style scoped>
.trend-canvas { width: 100%; height: 100%; display: block; }
</style>
