import { createApp } from 'vue'
import { createPinia } from 'pinia'
import naive from 'naive-ui'
import App from './App.vue'
import router from './router'
import './styles/operations.css'

// 入口文件：注册 Pinia、Vue Router、Naive UI 全局组件。
const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(naive)
app.mount('#app')
