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
        manualChunks: {
          vue: ['vue', 'vue-router', 'pinia'],
          'naive-ui': ['naive-ui'],
          axios: ['axios']
        }
      }
    }
  }
})
