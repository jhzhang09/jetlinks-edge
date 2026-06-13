<script setup lang="ts">
import { RouterView } from 'vue-router'
import { darkTheme } from 'naive-ui'
import { useThemeStore } from '@/stores/theme'
import { storeToRefs } from 'pinia'

const themeStore = useThemeStore()
const { theme } = storeToRefs(themeStore)
</script>
 
<template>
  <n-config-provider :theme="theme === 'dark' ? darkTheme : null">
    <n-message-provider>
      <n-notification-provider>
        <n-dialog-provider>
          <RouterView />
        </n-dialog-provider>
      </n-notification-provider>
    </n-message-provider>
  </n-config-provider>
</template>
