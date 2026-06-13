import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * 全局主题 Store
 * 支持白天模式 (light) 与夜间模式 (dark) 切换
 * @author jhzhang
 * @date 2026-06-09
 */
export const useThemeStore = defineStore('theme', () => {
  // 从本地缓存读取主题设置，默认使用 dark 夜间模式
  const theme = ref<'dark' | 'light'>((localStorage.getItem('theme') as 'dark' | 'light') || 'dark')

  /**
   * 切换日夜主题
   */
  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem('theme', theme.value)
    updateDocumentClass()
  }

  /**
   * 更新根 HTML 元素的属性标记
   */
  function updateDocumentClass() {
    if (theme.value === 'light') {
      document.documentElement.setAttribute('data-theme', 'light')
    } else {
      document.documentElement.removeAttribute('data-theme')
    }
  }

  // 页面初始化时执行一次绑定
  updateDocumentClass()

  return {
    theme,
    toggleTheme
  }
})
