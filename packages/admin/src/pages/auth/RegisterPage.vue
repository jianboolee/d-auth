<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NForm, NFormItem, NInput, NButton, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { useAuthPaths } from "../../paths";
import { errMessage } from "../../labels";
import AuthShell from "./AuthShell.vue";

const router = useRouter();
const client = useAdminClient();
const session = useAuthSession();
const paths = useAuthPaths();
const message = useMessage();
const email = ref("");
const code = ref("");
const password = ref("");
const nickname = ref("");
const sending = ref(false);
const loading = ref(false);

async function send() {
  sending.value = true;
  try {
    await client.sendEmail(email.value, "register");
    message.success("验证码已发送");
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    sending.value = false;
  }
}

async function submit() {
  loading.value = true;
  try {
    const res = await client.emailRegister(email.value, code.value, password.value, nickname.value);
    session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <AuthShell title="邮箱注册">
    <n-form :show-label="false" :show-feedback="false" class="form">
      <n-form-item>
        <n-input v-model:value="nickname" size="large" round placeholder="昵称：怎么称呼你" />
      </n-form-item>
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
          placeholder="密码：至少 8 位"
        />
      </n-form-item>
    </n-form>
    <div class="primary-action">
      <n-button type="primary" size="large" round block :loading="loading" @click="submit">
        注册
      </n-button>
    </div>
    <div class="alt-row">
      <n-button text type="primary" @click="router.push(paths.login())">返回登录</n-button>
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
