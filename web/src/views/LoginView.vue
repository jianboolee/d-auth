<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import {
  NCard,
  NTabs,
  NTabPane,
  NForm,
  NFormItem,
  NInput,
  NButton,
  NSpace,
  useMessage,
} from "naive-ui";
import { dauth } from "../api/client";
import { useAuthStore } from "../stores/auth";

const router = useRouter();
const auth = useAuthStore();
const message = useMessage();
const phone = ref("13800000000");
const smsCode = ref("");
const email = ref("");
const password = ref("");
const debug = ref("");
const qrUrl = ref("");
const ticket = ref("");
const sending = ref(false);
const loading = ref(false);

async function sendSms() {
  sending.value = true;
  try {
    const res = await dauth.sendSms(phone.value, "login");
    debug.value = res.debug_code ? `开发验证码 ${res.debug_code}` : "验证码已发送";
    message.success(debug.value);
  } catch (e: any) {
    message.error(e.message);
  } finally {
    sending.value = false;
  }
}

async function loginSms() {
  loading.value = true;
  try {
    const res = await dauth.smsLogin(phone.value, smsCode.value);
    auth.user = res.user;
    router.push("/account/profile");
  } catch (e: any) {
    message.error(e.message);
  } finally {
    loading.value = false;
  }
}

async function loginEmail() {
  loading.value = true;
  try {
    const res = await dauth.emailLogin(email.value, password.value);
    auth.user = res.user;
    router.push("/account/profile");
  } catch (e: any) {
    message.error(e.message);
  } finally {
    loading.value = false;
  }
}

async function startQr() {
  const res = await dauth.wechatQrStart();
  qrUrl.value = res.qr_url;
  ticket.value = res.ticket;
  const timer = window.setInterval(async () => {
    try {
      const st = await dauth.wechatQrStatus(ticket.value);
      if (st.status === "confirmed" && st.tokens) {
        window.clearInterval(timer);
        dauth.persistTokens(st.tokens);
        auth.user = st.tokens.user;
        router.push("/account/profile");
      }
    } catch {
      window.clearInterval(timer);
    }
  }, 1500);
}

async function mockScan() {
  await dauth.wechatMockScan(ticket.value);
  message.success("已模拟扫码，等待确认");
}
</script>

<template>
  <div class="wrap">
    <n-card class="card" title="d-auth 账户中心">
      <n-tabs type="line">
        <n-tab-pane name="sms" tab="手机验证码">
          <n-form>
            <n-form-item label="手机号"><n-input v-model:value="phone" /></n-form-item>
            <n-form-item label="验证码">
              <n-space>
                <n-input v-model:value="smsCode" />
                <n-button :loading="sending" @click="sendSms">发送</n-button>
              </n-space>
            </n-form-item>
            <n-button type="primary" block :loading="loading" @click="loginSms">登录 / 自动注册</n-button>
          </n-form>
        </n-tab-pane>
        <n-tab-pane name="email" tab="邮箱密码">
          <n-form>
            <n-form-item label="邮箱"><n-input v-model:value="email" /></n-form-item>
            <n-form-item label="密码"><n-input v-model:value="password" type="password" /></n-form-item>
            <n-button type="primary" block :loading="loading" @click="loginEmail">登录</n-button>
            <n-space style="margin-top: 12px">
              <n-button text @click="router.push('/register')">注册</n-button>
              <n-button text @click="router.push('/forgot')">忘记密码</n-button>
            </n-space>
          </n-form>
        </n-tab-pane>
        <n-tab-pane name="wechat" tab="微信扫码">
          <n-space vertical>
            <n-button @click="startQr">获取二维码</n-button>
            <a v-if="qrUrl" :href="qrUrl" target="_blank">打开微信授权</a>
            <n-button v-if="ticket" secondary @click="mockScan">开发环境模拟扫码</n-button>
          </n-space>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </div>
</template>

<style scoped>
.wrap {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}
.card {
  width: min(440px, 100%);
}
</style>
