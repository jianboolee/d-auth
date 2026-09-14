<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { NForm, NFormItem, NInput, NButton } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { useAuthPaths } from "../../paths";
import { errMessage } from "../../labels";
import AuthShell from "./AuthShell.vue";

const router = useRouter();
const client = useAdminClient();
const session = useAuthSession();
const paths = useAuthPaths();
const { user } = session;

const phone = ref("");
const code = ref("");
const sending = ref(false);
const loading = ref(false);
const countdown = ref(0);
const tip = ref("");
const tipKind = ref<"info" | "error">("info");

function setTip(text: string, kind: "info" | "error" = "info") {
  tip.value = text;
  tipKind.value = kind;
}

const phoneValid = computed(() => /^1\d{10}$/.test(phone.value.trim()));
const canSend = computed(() => phoneValid.value && !sending.value && countdown.value === 0);
const sendLabel = computed(() => (countdown.value > 0 ? `${countdown.value}s 后重发` : "发送验证码"));

let timer: number | null = null;

function clearCountdown() {
  if (timer != null) {
    window.clearInterval(timer);
    timer = null;
  }
}

function startCountdown(seconds = 60) {
  clearCountdown();
  countdown.value = seconds;
  timer = window.setInterval(() => {
    countdown.value -= 1;
    if (countdown.value <= 0) {
      countdown.value = 0;
      clearCountdown();
    }
  }, 1000);
}

onUnmounted(clearCountdown);

function normalizePhone(value: string) {
  return value.replace(/\D/g, "").slice(0, 11);
}

async function send() {
  setTip("");
  if (!phoneValid.value) {
    setTip("请输入正确的 11 位手机号", "error");
    return;
  }
  sending.value = true;
  try {
    await client.sendSms(phone.value.trim(), "bind_phone");
    setTip("验证码已发送，请查收短信");
    startCountdown(60);
  } catch (e: unknown) {
    setTip(errMessage(e), "error");
  } finally {
    sending.value = false;
  }
}

async function submit() {
  setTip("");
  if (!phoneValid.value) {
    setTip("请输入正确的 11 位手机号", "error");
    return;
  }
  if (!code.value.trim()) {
    setTip("请输入验证码", "error");
    return;
  }
  loading.value = true;
  try {
    await client.changePhone({ new_phone: phone.value.trim(), new_code: code.value.trim() });
    // 绑定成功后刷新登录态，让 has_phone 立即变为 true，再回到资料页。
    const res = await client.session();
    if (res.user) session.setUser(res.user);
    router.push(paths.profile());
  } catch (e: unknown) {
    setTip(errMessage(e), "error");
  } finally {
    loading.value = false;
  }
}

async function signOut() {
  try {
    await client.logout();
  } catch {
    // 退出失败也要清理本地状态并回登录页
  }
  session.setUser(null);
  router.push(paths.login());
}
</script>

<template>
  <AuthShell title="绑定手机号">
    <p class="hint">手机号将作为你的主要登录方式，绑定后可用验证码登录。</p>
    <n-form :show-label="false" :show-feedback="false" class="form">
      <n-form-item>
        <n-input
          :value="phone"
          size="large"
          round
          maxlength="11"
          inputmode="numeric"
          placeholder="请输入手机号"
          @update:value="phone = normalizePhone($event)"
        >
          <template #prefix>
            <span class="phone-prefix">+86</span>
          </template>
        </n-input>
      </n-form-item>
      <n-form-item>
        <div class="dauth-code-row">
          <n-input
            v-model:value="code"
            size="large"
            round
            maxlength="6"
            inputmode="numeric"
            placeholder="请输入验证码"
            @keyup.enter="submit"
          />
          <n-button size="large" round :loading="sending" :disabled="!canSend && !sending" @click="send">
            {{ sendLabel }}
          </n-button>
        </div>
      </n-form-item>
    </n-form>

    <p class="tip" :class="tip ? tipKind : 'empty'">{{ tip || "\u00a0" }}</p>

    <div class="primary-action">
      <n-button type="primary" size="large" round block :loading="loading" @click="submit">
        绑定
      </n-button>
    </div>
    <div class="alt-row">
      <n-button text @click="signOut">退出登录（{{ user?.nickname || "当前账号" }}）</n-button>
    </div>
  </AuthShell>
</template>

<style scoped>
.hint {
  margin: 0 0 16px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--dauth-text-secondary, #6b6b6b);
  text-align: center;
}
.form {
  margin-bottom: 0;
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
.alt-row {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}
</style>
