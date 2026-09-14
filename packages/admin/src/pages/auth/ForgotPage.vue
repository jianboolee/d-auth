<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NCard, NForm, NFormItem, NInput, NButton, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthPaths } from "../../paths";
import { errMessage } from "../../labels";

const router = useRouter();
const client = useAdminClient();
const paths = useAuthPaths();
const message = useMessage();
const email = ref("");
const code = ref("");
const password = ref("");
const isDev = import.meta.env.DEV;

async function send() {
  try {
    const res = await client.sendEmail(email.value, "reset_password");
    message.success(isDev && res.debug_code ? `开发验证码 ${res.debug_code}` : "如邮箱存在将收到验证码");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}

async function submit() {
  try {
    await client.resetPassword(email.value, code.value, password.value);
    message.success("密码已重置");
    router.push(paths.login());
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
</script>

<template>
  <div class="wrap">
    <n-card title="重置密码" class="card">
      <n-form>
        <n-form-item label="邮箱"><n-input v-model:value="email" placeholder="请输入邮箱" /></n-form-item>
        <n-form-item label="验证码">
          <div class="dauth-code-row">
            <n-input v-model:value="code" placeholder="请输入验证码" />
            <n-button @click="send">
              <template #icon><i class="ri-send-plane-line dauth-ri" /></template>
              发送
            </n-button>
          </div>
        </n-form-item>
        <n-form-item label="新密码"><n-input v-model:value="password" type="password" placeholder="至少 8 位" /></n-form-item>
        <n-button type="primary" block @click="submit">重置</n-button>
        <n-button text style="margin-top: 12px" @click="router.push(paths.login())">返回登录</n-button>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
.wrap {
  min-height: calc(100vh - 32px);
  display: grid;
  place-items: center;
  padding: 16px;
}
.card {
  width: min(440px, 100%);
}
</style>
