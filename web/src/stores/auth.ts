import { defineStore } from 'pinia'
import { login, me, type LoginResp } from '@/api'

// 简单的登录态：localStorage 中持久化 token。
export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('jetlinks-edge-token') || '',
    user: null as null | { id: string; username: string; role: string }
  }),
  getters: {
    isLoggedIn: (s) => !!s.token
  },
  actions: {
    async login(username: string, password: string) {
      const resp: LoginResp = await login({ username, password })
      this.token = resp.token
      this.user = resp.user
      localStorage.setItem('jetlinks-edge-token', resp.token)
      return resp
    },
    async loadProfile() {
      if (!this.token) return
      try {
        this.user = await me()
      } catch {
        this.logout()
      }
    },
    logout() {
      this.token = ''
      this.user = null
      localStorage.removeItem('jetlinks-edge-token')
    }
  }
})
