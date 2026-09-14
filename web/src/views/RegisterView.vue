<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NCard, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from "naive-ui";
import { dauth } from "../api/client";
import { useAuthStore } from "../stores/auth";

const router = useRouter();
const auth = useAuthStore();
const message = useMessage();
const email = ref("");
const code = ref("");
const password = ref("");
const nickname = ref("");
const sending = ref(false);

async function send() {
  sending.value = true;
  try {
    const res = await dauth.sendEmail(email.value, "register");
    message.success(res.debug_code ? `开发验证码 ${res.debug_code}` : "验证码已发送");
  } catch (e: any) {
    message.error(e.message);
  } finally {
    sending.value = false;
  }
}

async function submit() {
  try {
    const res = await dauth.emailRegister(email.value, code.value, password.value, nickname.value);
    auth.user = res.user;
    router.push("/account/profile");
  } catch (e: any) {
    message.error(e.message);
  }
}
</script>

<template>
  <div class="wrap">
    <n-card title="邮箱注册" style="width: min(440px, 100%)">
      <n-form>
        <n-form-item label="昵称"><n-input v-model:value="nickname" /></n-form-item>
        <n-form-item label="邮箱"><n-input v-model:value="email" /></n-form-item>
        <n-form-item label="验证码">
          <n-space>
            <n-input v-model:value="code" />
            <n-button :loading="sending" @click="send">发送</n-button>
          </n-space>
        </n-form-item>
        <n-form-item label="密码"><n-input v-model:value="password" type="password" /></n-form-item>
        <n-button type="primary" block @click="submit">注册</n-button>
        <n-button text style="margin-top: 12px" @click="router.push('/login')">返回登录</n-button>
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
