import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 路由表：除 /login 外所有页面都需要登录。
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true }
    },
    {
      path: '/',
      component: () => import('@/views/LayoutView.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'topology', name: 'topology', component: () => import('@/views/TopologyView.vue') },
        { path: 'alarms', name: 'alarms', component: () => import('@/views/AlarmCenterView.vue') },
        { path: 'connections', name: 'connections', component: () => import('@/views/ConnectionsView.vue') },
        { path: 'groups', name: 'groups', component: () => import('@/views/GroupsView.vue') },
        { path: 'groups/:id', name: 'group-detail', component: () => import('@/views/GroupDetailView.vue') },
        { path: 'northbound', name: 'northbound', component: () => import('@/views/NorthboundView.vue') }
      ]
    }
  ]
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.isLoggedIn) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && auth.isLoggedIn) {
    return { name: 'dashboard' }
  }
})

export default router
