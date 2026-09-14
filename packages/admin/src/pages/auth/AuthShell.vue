<script setup lang="ts">
import { NCard } from "naive-ui";

defineProps<{
  title?: string;
}>();
</script>

<template>
  <div class="auth-wrap">
    <n-card class="auth-card" :bordered="false">
      <div class="auth-brand">
        <img class="auth-logo" src="/logo.png" alt="logo" />
        <h1 v-if="title" class="auth-title">{{ title }}</h1>
      </div>
      <div class="auth-body">
        <slot />
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.auth-wrap {
  min-height: calc(100vh - 32px);
  display: grid;
  place-items: center;
  padding: 16px;
}
.auth-card {
  width: min(420px, 100%);
  background: transparent;
}
.auth-card :deep(.n-card__content) {
  padding: 8px 4px 16px;
}
.auth-brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  margin-bottom: 24px;
}
.auth-logo {
  width: 72px;
  height: 72px;
  object-fit: contain;
  border-radius: 18px;
}
.auth-title {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.auth-body {
  display: flex;
  flex-direction: column;
}
.auth-body :deep(.n-form-item) {
  margin-bottom: 16px;
}
.auth-body :deep(.n-form-item:last-child) {
  margin-bottom: 0;
}
/* naive-ui 将 --n-height / --n-font-size 以内联样式写在组件根上，
   普通 CSS 变量声明会被内联样式覆盖，需用 !important 才能生效。 */
.auth-body :deep(.n-input) {
  --n-height: 48px !important;
  --n-font-size: 16px !important;
}
.auth-body :deep(.dauth-code-row .n-button) {
  --n-height: 48px !important;
  --n-font-size: 15px !important;
  --n-padding: 0 18px !important;
  flex-shrink: 0;
  min-width: 118px;
}
.auth-body :deep(.primary-action .n-button) {
  --n-height: 48px !important;
  --n-font-size: 16px !important;
}
</style>
