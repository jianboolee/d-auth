<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NCard, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from "naive-ui";
import { dauth } from "../api/client";

const router = useRouter();
const message = useMessage();
const email = ref("");
const code = ref("");
const password = ref("");

async function send() {
  try {
    const res = await dauth.sendEmail(email.value, "reset_password");
    message.success(res.debug_code ? `开发验证码 ${res.debug_code}` : "如邮箱存在将收到验证码");
  } catch (e: any) {
    message.error(e.message);
  }
}

async function submit() {
  try {
    await dauth.resetPassword(email.value, code.value, password.value);
    message.success("密码已重置");
    router.push("/login");
  } catch (e: any) {
    message.error(e.message);
  }
}
</script>

<template>
  <div class="wrap">
    <n-card title="重置密码" style="width: min(440px, 100%)">
      <n-form>
        <n-form-item label="邮箱"><n-input v-model:value="email" /></n-form-item>
        <n-form-item label="验证码">
          <n-space>
            <n-input v-model:value="code" />
            <n-button @click="send">发送</n-button>
          </n-space>
        </n-form-item>
        <n-form-item label="新密码"><n-input v-model:value="password" type="password" /></n-form-item>
        <n-button type="primary" block @click="submit">重置</n-button>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
.wrap {
  min-height: 100vh;
  display: grid;
  place-items: center;
}
</style>
