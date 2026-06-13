<script setup lang="ts">
import { NFormItem, NInput, NInputNumber, NSelect, NSwitch, NText } from 'naive-ui'
import type { ConfigField } from '@/api'

const props = defineProps<{
  modelValue: Record<string, any>
  schema: ConfigField[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: Record<string, any>]
}>()

function update(key: string, value: any) {
  emit('update:modelValue', { ...(props.modelValue || {}), [key]: value })
}

function selectOptions(field: ConfigField): any[] {
  return field.options || []
}
</script>

<template>
  <template v-for="field in schema" :key="field.key">
    <n-form-item :label="field.label" :required="field.required">
      <n-input-number
        v-if="field.type === 'number'"
        :value="modelValue?.[field.key]"
        :min="field.min"
        :max="field.max"
        :step="field.step"
        :placeholder="field.placeholder"
        style="width: 100%"
        @update:value="update(field.key, $event)"
      />
      <n-switch
        v-else-if="field.type === 'boolean'"
        :value="modelValue?.[field.key]"
        @update:value="update(field.key, $event)"
      />
      <n-select
        v-else-if="field.type === 'select'"
        :value="modelValue?.[field.key]"
        :options="selectOptions(field)"
        :placeholder="field.placeholder"
        @update:value="update(field.key, $event)"
      />
      <n-input
        v-else
        :value="modelValue?.[field.key]"
        :type="field.type === 'password' ? 'password' : field.type === 'textarea' ? 'textarea' : 'text'"
        :placeholder="field.placeholder"
        :show-password-on="field.type === 'password' ? 'click' : undefined"
        @update:value="update(field.key, $event)"
      />
      <n-text v-if="field.description" depth="3" class="description">{{ field.description }}</n-text>
    </n-form-item>
  </template>
</template>

<style scoped>
.description {
  display: block;
  margin-top: 4px;
  font-size: 13px;
}
</style>
