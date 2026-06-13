import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'node:path'

// 边缘网关前端构建配置。
// 开发时通过 vite proxy 把 /api 转发到 127.0.0.1:7001。
// 生产构建产物在 web/dist 目录，JetLinks Edge 启动时会挂载为静态文件。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src')
    }
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7001',
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // 边缘网关前端 chunk 切分策略。
        // vite 8 + rolldown 已不再支持 manualChunks 对象字面量，必须改为 ManualChunksFunction。
        // 这里按包名前缀将 vue 全家桶、naive-ui、axios 拆为独立 chunk，便于浏览器长期缓存。
        manualChunks: (id) => {
          if (/node_modules\/(vue|vue-router|pinia)\//.test(id)) {
            return 'vue'
          }
          if (/node_modules\/naive-ui\//.test(id)) {
            return 'naive-ui'
          }
          if (/node_modules\/axios\//.test(id)) {
            return 'axios'
          }
          return undefined
        }
      }
    }
  }
})
