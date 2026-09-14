<script setup lang="ts">
import { h, ref } from "vue";
import { useRouter } from "vue-router";
import { NCard, NTabs, NTabPane, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { useAuthPaths } from "../../paths";
import { errMessage } from "../../labels";

const router = useRouter();
const client = useAdminClient();
const session = useAuthSession();
const paths = useAuthPaths();
const message = useMessage();
const phone = ref("");
const smsCode = ref("");
const email = ref("");
const password = ref("");
const qrUrl = ref("");
const ticket = ref("");
const sending = ref(false);
const loading = ref(false);
const isDev = import.meta.env.DEV;

function tabLabel(icon: string, text: string) {
  return h("span", { class: "tab-lab" }, [h("i", { class: `${icon} dauth-ri` }), " ", text]);
}

async function sendSms() {
  sending.value = true;
  try {
    const res = await client.sendSms(phone.value, "login");
    message.success(isDev && res.debug_code ? `开发验证码 ${res.debug_code}` : "验证码已发送");
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    sending.value = false;
  }
}

async function loginSms() {
  loading.value = true;
  try {
    const res = await client.smsLogin(phone.value, smsCode.value);
    session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}

async function loginEmail() {
  loading.value = true;
  try {
    const res = await client.emailLogin(email.value, password.value);
    session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}

async function startQr() {
  const res = await client.wechatQrStart();
  qrUrl.value = res.qr_url;
  ticket.value = res.ticket;
  const timer = window.setInterval(async () => {
    try {
      const st = await client.wechatQrStatus(ticket.value);
      if (st.status === "confirmed" && st.tokens) {
        window.clearInterval(timer);
        client.persistTokens(st.tokens);
        session.setUser(st.tokens.user);
        router.push(paths.profile());
      }
    } catch {
      window.clearInterval(timer);
    }
  }, 1500);
}

async function mockScan() {
  await client.wechatMockScan(ticket.value);
  message.success("已模拟扫码，等待确认");
}
</script>

<template>
  <div class="wrap">
    <n-card class="card" title="登录">
      <n-tabs type="line">
        <n-tab-pane name="sms" :tab="tabLabel('ri-smartphone-line', '手机验证码')">
          <n-form>
            <n-form-item label="手机号">
              <n-input v-model:value="phone" placeholder="请输入手机号" />
            </n-form-item>
            <n-form-item label="验证码">
              <div class="dauth-code-row">
                <n-input v-model:value="smsCode" placeholder="请输入验证码" />
                <n-button :loading="sending" @click="sendSms">
                  <template #icon><i class="ri-send-plane-line dauth-ri" /></template>
                  发送
                </n-button>
              </div>
            </n-form-item>
            <n-button type="primary" block :loading="loading" @click="loginSms">登录 / 自动注册</n-button>
          </n-form>
        </n-tab-pane>
        <n-tab-pane name="email" :tab="tabLabel('ri-mail-line', '邮箱密码')">
          <n-form>
            <n-form-item label="邮箱"><n-input v-model:value="email" placeholder="请输入邮箱" /></n-form-item>
            <n-form-item label="密码"><n-input v-model:value="password" type="password" placeholder="请输入密码" /></n-form-item>
            <n-button type="primary" block :loading="loading" @click="loginEmail">登录</n-button>
            <n-space style="margin-top: 12px">
              <n-button text @click="router.push(paths.register())">注册</n-button>
              <n-button text @click="router.push(paths.forgot())">忘记密码</n-button>
            </n-space>
          </n-form>
        </n-tab-pane>
        <n-tab-pane name="wechat" :tab="tabLabel('ri-wechat-line', '微信扫码')">
          <n-space vertical>
            <n-button @click="startQr">
              <template #icon><i class="ri-qr-code-line dauth-ri" /></template>
              获取二维码
            </n-button>
            <a v-if="qrUrl" :href="qrUrl" target="_blank">打开微信授权</a>
            <n-button v-if="isDev && ticket" secondary @click="mockScan">开发环境模拟扫码</n-button>
          </n-space>
        </n-tab-pane>
      </n-tabs>
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
.tab-lab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>
