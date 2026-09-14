<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { NForm, NFormItem, NInput, NButton, NSpace } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { useAuthPaths } from "../../paths";
import { toLocalCNPhone } from "../../phone";
import { isRateLimited } from "../../ratelimit";
import { errMessage } from "../../labels";
import AuthShell from "./AuthShell.vue";

type LoginMode = "sms" | "password" | "wechat";

const router = useRouter();
const client = useAdminClient();
const session = useAuthSession();
const paths = useAuthPaths();
const mode = ref<LoginMode>("sms");
const phone = ref("");
const smsCode = ref("");
const identifier = ref("");
const password = ref("");
const sending = ref(false);
const loading = ref(false);
const countdown = ref(0);
const tip = ref("");
const tipKind = ref<"info" | "error">("info");

function setTip(text: string, kind: "info" | "error" = "info") {
  tip.value = text;
  tipKind.value = kind;
}

function clearTip() {
  tip.value = "";
}

let countdownTimer: number | null = null;

const phoneValid = computed(() => /^1\d{10}$/.test(phone.value.trim()));
const canSendSms = computed(() => phoneValid.value && !sending.value && countdown.value === 0);
const sendLabel = computed(() =>
  countdown.value > 0 ? `${countdown.value}s 后重发` : "发送验证码",
);

function clearCountdown() {
  if (countdownTimer != null) {
    window.clearInterval(countdownTimer);
    countdownTimer = null;
  }
}

function startCountdown(seconds = 60) {
  clearCountdown();
  countdown.value = seconds;
  countdownTimer = window.setInterval(() => {
    countdown.value -= 1;
    if (countdown.value <= 0) {
      countdown.value = 0;
      clearCountdown();
    }
  }, 1000);
}

onUnmounted(clearCountdown);

function onPhoneUpdate(value: string) {
  // 输入框上固定显示 +86，粘贴完整号码（+86/86 开头）时先去掉国家码，
  // 否则会被截成 86138001380。
  phone.value = toLocalCNPhone(value);
  clearTip();
}

async function sendSms() {
  clearTip();
  if (!phoneValid.value) {
    setTip("请输入正确的手机号", "error");
    return;
  }
  sending.value = true;
  try {
    await client.sendSms(phone.value.trim(), "login");
    setTip("验证码已发送，请查收短信");
    startCountdown(60);
  } catch (e: unknown) {
    setTip(errMessage(e), "error");
    // 命中限流时也进入倒计时，避免用户反复点击被连续拒绝。
    // 注意这纯粹是体验优化，真正的限制在服务端。
    if (isRateLimited(e)) startCountdown(60);
  } finally {
    sending.value = false;
  }
}

async function loginSms() {
  clearTip();
  if (!phoneValid.value) {
    setTip("请输入正确的手机号", "error");
    return;
  }
  if (!smsCode.value.trim()) {
    setTip("请输入验证码", "error");
    return;
  }
  loading.value = true;
  try {
    const res = await client.smsLogin(phone.value.trim(), smsCode.value.trim());
    session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    setTip(errMessage(e), "error");
  } finally {
    loading.value = false;
  }
}

async function loginPassword() {
  clearTip();
  if (!identifier.value.trim() || !password.value) {
    setTip("请输入手机号/邮箱和密码", "error");
    return;
  }
  loading.value = true;
  try {
    const res = await client.passwordLogin(identifier.value.trim(), password.value);
    session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    setTip(errMessage(e), "error");
  } finally {
    loading.value = false;
  }
}

function switchMode(next: LoginMode) {
  mode.value = next;
  clearTip();
}
</script>

<template>
  <AuthShell>
    <div class="login-body">
      <div class="mode-panel">
        <n-form v-show="mode === 'sms'" :show-label="false" :show-feedback="false" class="form">
          <n-form-item>
            <n-input
              :value="phone"
              size="large"
              round
              inputmode="tel"
              placeholder="请输入手机号"
              @update:value="onPhoneUpdate"
            >
              <template #prefix>
                <span class="phone-prefix">+86</span>
              </template>
            </n-input>
          </n-form-item>
          <n-form-item>
            <div class="dauth-code-row">
              <n-input
                v-model:value="smsCode"
                size="large"
                round
                maxlength="8"
                inputmode="numeric"
                placeholder="请输入验证码"
              />
              <n-button
                size="large"
                round
                :loading="sending"
                :disabled="!canSendSms && !sending"
                @click="sendSms"
              >
                {{ sendLabel }}
              </n-button>
            </div>
          </n-form-item>
        </n-form>

        <n-form v-show="mode === 'password'" :show-label="false" :show-feedback="false" class="form">
          <n-form-item>
            <n-input
              v-model:value="identifier"
              size="large"
              round
              placeholder="请输入手机号或邮箱"
              @keyup.enter="loginPassword"
            />
          </n-form-item>
          <n-form-item>
            <n-input
              v-model:value="password"
              size="large"
              round
              type="password"
              show-password-on="click"
              placeholder="请输入密码"
              @keyup.enter="loginPassword"
            />
          </n-form-item>
        </n-form>

        <div v-show="mode === 'wechat'" class="wechat-panel">
          <div class="qr-placeholder" aria-hidden="true" />
          <p class="qr-hint">请使用微信扫一扫登录</p>
        </div>
      </div>

      <p v-if="mode !== 'wechat'" class="agree">
        注册登录即代表已阅读并同意我们的
        <a :href="paths.terms()" target="_blank" rel="noopener noreferrer">用户协议</a>
        与
        <a :href="paths.privacy()" target="_blank" rel="noopener noreferrer">隐私政策</a><template v-if="mode === 'sms'">，未注册的手机号将自动注册。</template><template v-else>。</template>
      </p>

      <p class="tip" :class="tip ? tipKind : 'empty'">{{ tip || "\u00a0" }}</p>

      <div v-if="mode !== 'wechat'" class="primary-action">
        <n-button
          v-if="mode === 'sms'"
          type="primary"
          size="large"
          round
          block
          :loading="loading"
          @click="loginSms"
        >
          登录
        </n-button>
        <n-button
          v-else
          type="primary"
          size="large"
          round
          block
          :loading="loading"
          @click="loginPassword"
        >
          登录
        </n-button>
      </div>

      <div v-if="mode === 'password'" class="secondary-action">
        <n-space justify="space-between" style="width: 100%">
          <n-button text @click="router.push(paths.register())">注册</n-button>
          <n-button text @click="router.push(paths.forgot())">忘记密码</n-button>
        </n-space>
      </div>

      <div class="alt-row">
        <template v-if="mode === 'sms'">
          <n-button text type="primary" @click="switchMode('password')">密码登录</n-button>
          <n-button text type="primary" @click="switchMode('wechat')">微信登录</n-button>
        </template>
        <template v-else-if="mode === 'password'">
          <n-button text type="primary" @click="switchMode('sms')">验证码登录</n-button>
          <n-button text type="primary" @click="switchMode('wechat')">微信登录</n-button>
        </template>
        <template v-else>
          <n-button text type="primary" @click="switchMode('sms')">验证码登录</n-button>
          <n-button text type="primary" @click="switchMode('password')">密码登录</n-button>
        </template>
      </div>
    </div>
  </AuthShell>
</template>

<style scoped>
.login-body {
  display: flex;
  flex-direction: column;
  /* 三种模式内容高度不同，卡片又是垂直居中，会导致切换时整卡（含 logo）上下移动。
     锁定为最高模式（密码登录）的高度，使卡片等高、logo 位置稳定。 */
  min-height: 330px;
}
.mode-panel {
  display: flex;
  flex-direction: column;
}
.phone-prefix {
  color: var(--dauth-text, #1a1a1a);
  font-size: 15px;
  font-weight: 500;
  padding-right: 8px;
  border-right: 1px solid var(--dauth-border, #ececec);
  margin-right: 8px;
  line-height: 1;
}
.wechat-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 4px 0 8px;
}
.qr-placeholder {
  width: 128px;
  height: 128px;
  border-radius: 12px;
  background: #e8e8e8;
}
.qr-hint {
  margin: 0;
  font-size: 13px;
  line-height: 1.4;
  color: var(--dauth-text-secondary, #6b6b6b);
}
.agree {
  margin: 16px 0 0;
  min-height: calc(12px * 1.6 * 2);
  color: var(--dauth-text-secondary, #8c8c8c);
  font-size: 12px;
  line-height: 1.6;
  text-align: left;
}
.agree a {
  color: var(--dauth-text, #1a1a1a);
  text-decoration: none;
}
.agree a:hover {
  text-decoration: underline;
}
.tip {
  margin: 8px 0 0;
  min-height: calc(13px * 1.5);
  font-size: 13px;
  line-height: 1.5;
}
.tip.info {
  color: #16a34a;
}
.tip.error {
  color: #dc2626;
}
.tip.empty {
  visibility: hidden;
}
.primary-action {
  margin-top: 16px;
  min-height: 48px;
}
.secondary-action {
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 12px;
}
/* naive-ui 将文字色以内联样式写在按钮根上，需 !important 覆盖 */
.secondary-action :deep(.n-button) {
  --n-text-color: var(--dauth-text-secondary, #8c8c8c) !important;
  --n-text-color-hover: var(--dauth-text, #1a1a1a) !important;
  --n-text-color-pressed: var(--dauth-text, #1a1a1a) !important;
  --n-text-color-focus: var(--dauth-text, #1a1a1a) !important;
}
.alt-row {
  display: flex;
  justify-content: center;
  gap: 20px;
  margin-top: 24px;
}
</style>
