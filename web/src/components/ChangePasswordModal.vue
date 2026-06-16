<script setup lang="ts">
/**
 * 修改密码弹窗组件
 * @author jhzhang
 * @date 2026-06-16
 */
import { ref } from 'vue'
import { useMessage, NModal, NForm, NFormItem, NInput, NButton } from 'naive-ui'
import { useI18n } from '@/i18n'
import { changePassword } from '@/api'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  (e: 'update:show', val: boolean): void
  (e: 'success'): void
}>()

const { t } = useI18n()
const message = useMessage()
const submitLoading = ref(false)

const formValue = ref({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const handleUpdateShow = (val: boolean) => {
  emit('update:show', val)
}

async function submitPasswordChange() {
  if (!formValue.value.oldPassword) {
    message.error(t('top.old_password_required'))
    return
  }
  if (!formValue.value.newPassword) {
    message.error(t('top.new_password_required'))
    return
  }
  if (formValue.value.newPassword !== formValue.value.confirmPassword) {
    message.error(t('top.passwords_not_match'))
    return
  }
  
  submitLoading.value = true
  try {
    await changePassword({
      oldPassword: formValue.value.oldPassword,
      newPassword: formValue.value.newPassword
    })
    message.success(t('top.password_changed_success'))
    emit('update:show', false)
    emit('success')
  } catch (e: any) {
    const errMsg = e?.response?.data?.error || t('top.password_changed_failed')
    message.error(errMsg)
  } finally {
    submitLoading.value = false
  }
}
</script>

<template>
  <n-modal
    :show="props.show"
    @update:show="handleUpdateShow"
    preset="card"
    style="width: 400px;"
    :title="t('top.change_password_title')"
    :bordered="false"
    size="huge"
    class="ops-dialog"
  >
    <n-form :model="formValue">
      <n-form-item :label="t('top.old_password')">
        <n-input
          v-model:value="formValue.oldPassword"
          type="password"
          show-password-on="mousedown"
          :placeholder="t('top.old_password_required')"
        />
      </n-form-item>
      <n-form-item :label="t('top.new_password')">
        <n-input
          v-model:value="formValue.newPassword"
          type="password"
          show-password-on="mousedown"
          :placeholder="t('top.new_password_required')"
        />
      </n-form-item>
      <n-form-item :label="t('top.confirm_new_password')">
        <n-input
          v-model:value="formValue.confirmPassword"
          type="password"
          show-password-on="mousedown"
          :placeholder="t('top.confirm_new_password_required')"
          @keyup.enter="submitPasswordChange"
        />
      </n-form-item>
    </n-form>
    <template #action>
      <div style="display: flex; justify-content: flex-end; gap: 12px;">
        <n-button @click="handleUpdateShow(false)">{{ t('top.cancel') }}</n-button>
        <n-button type="primary" :loading="submitLoading" @click="submitPasswordChange">
          {{ t('top.submit') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>
