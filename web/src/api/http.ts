import axios, { type AxiosInstance } from 'axios'
import { STORAGE_TOKEN_KEY } from '@/constants/keys'

// axios 实例：自动从 localStorage 读取 token，写入 Authorization 头。
const http: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 10_000
})

http.interceptors.request.use((cfg) => {
  const tk = localStorage.getItem(STORAGE_TOKEN_KEY)
  if (tk) {
    cfg.headers.set('Authorization', `Bearer ${tk}`)
  }
  return cfg
})

http.interceptors.response.use(
  (resp) => resp,
  (err) => {
    if (err?.response?.status === 401) {
      localStorage.removeItem(STORAGE_TOKEN_KEY)
      if (location.pathname !== '/login') {
        location.href = '/login'
      }
    }
    return Promise.reject(err)
  }
)

export default http
