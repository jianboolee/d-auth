<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NForm, NFormItem, NInput, NButton, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthPaths } from "../../paths";
import { errMessage } from "../../labels";
import AuthShell from "./AuthShell.vue";

const router = useRouter();
const route = useRoute();
const client = useAdminClient();
const paths = useAuthPaths();
const message = useMessage();
const email = ref("");
const code = ref("");
const password = ref("");
const sending = ref(false);
const loading = ref(false);

// 回登录页透传 client/return_to：SSO 流程中重置完密码，登录成功仍能回跳子系统
function goLogin() {
  router.push({ path: paths.login(), query: route.query });
}

async function send() {
  sending.value = true;
  try {
    await client.sendEmail(email.value, "reset_password");
    message.success("如邮箱存在将收到验证码");
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    sending.value = false;
  }
}

async function submit() {
  loading.value = true;
  try {
    await client.resetPassword(email.value, code.value, password.value);
    message.success("密码已重置");
    goLogin();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <AuthShell title="重置密码">
    <n-form :show-label="false" :show-feedback="false" class="form">
      <n-form-item>
        <n-input v-model:value="email" size="large" round placeholder="请输入邮箱" />
      </n-form-item>
      <n-form-item>
        <div class="dauth-code-row">
          <n-input v-model:value="code" size="large" round placeholder="请输入验证码" />
          <n-button size="large" round :loading="sending" @click="send">
            发送验证码
          </n-button>
        </div>
      </n-form-item>
      <n-form-item>
        <n-input
          v-model:value="password"
          size="large"
          round
          type="password"
          show-password-on="click"
          placeholder="新密码：至少 8 位"
        />
      </n-form-item>
    </n-form>
    <div class="primary-action">
      <n-button type="primary" size="large" round block :loading="loading" @click="submit">
        重置密码
      </n-button>
    </div>
    <div class="alt-row">
      <n-button text type="primary" @click="goLogin">返回登录</n-button>
    </div>
  </AuthShell>
</template>

<style scoped>
.form {
  margin-bottom: 16px;
}
.primary-action {
  min-height: 48px;
}
.alt-row {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}
</style>
