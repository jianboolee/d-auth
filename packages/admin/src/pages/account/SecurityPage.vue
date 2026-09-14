<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { NButton, NDrawer, NDrawerContent, NForm, NFormItem, NInput, useDialog, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { errMessage } from "../../labels";

const client = useAdminClient();
const session = useAuthSession();
const { user } = session;
const message = useMessage();
const dialog = useDialog();

type Panel = "password" | "phone" | "email";
const panel = ref<Panel>("password");
const show = ref(false);
const busy = ref(false);

const pw = reactive({ old: "", next: "", confirm: "" });
const ph = reactive({ oldCode: "", next: "", newCode: "", sendingOld: false, sendingNew: false });
const em = reactive({ next: "", code: "", sending: false });

const phoneValid = computed(() => /^1\d{10}$/.test(ph.next.trim()));
const emailValid = computed(() => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(em.next.trim()));
const hasPassword = computed(() => !!user.value?.has_password);
const boundPhone = computed(() => user.value?.phone ?? "");
const boundEmail = computed(() => user.value?.email ?? "");

function maskPhone(raw?: string) {
  if (!raw) return "";
  const digits = raw.replace(/\D/g, "");
  const local = digits.startsWith("86") && digits.length > 11 ? digits.slice(2) : digits;
  if (local.length !== 11) return raw;
  return `${local.slice(0, 3)}****${local.slice(7)}`;
}

function maskEmail(raw?: string) {
  if (!raw) return "";
  const [local, domain] = raw.split("@");
  if (!domain) return raw;
  return `${local.slice(0, 3)}***@${domain}`;
}

/** 独立的 60s 重发倒计时，每个验证码字段一份。 */
function useCountdown() {
  const seconds = ref(0);
  let timer: number | null = null;
  function clear() {
    if (timer != null) {
      window.clearInterval(timer);
      timer = null;
    }
  }
  function start(total = 60) {
    clear();
    seconds.value = total;
    timer = window.setInterval(() => {
      seconds.value -= 1;
      if (seconds.value <= 0) {
        seconds.value = 0;
        clear();
      }
    }, 1000);
  }
  onUnmounted(clear);
  return { seconds, start, clear };
}

const { seconds: oldSeconds, start: startOldCd, clear: clearOldCd } = useCountdown();
const { seconds: newSeconds, start: startNewCd, clear: clearNewCd } = useCountdown();
const { seconds: emailSeconds, start: startEmailCd, clear: clearEmailCd } = useCountdown();

function cdLabel(seconds: number) {
  return seconds > 0 ? `${seconds}s 后重发` : "发送验证码";
}

const rows = computed(() => [
  {
    key: "password" as Panel,
    icon: "ri-lock-password-line",
    title: "登录密码",
    sub: hasPassword.value ? "已设置" : "未设置，建议设置密码以便登录",
    action: hasPassword.value ? "修改密码" : "设置密码",
  },
  {
    key: "phone" as Panel,
    icon: "ri-smartphone-line",
    title: "手机号",
    sub: boundPhone.value ? `已绑定 ${maskPhone(boundPhone.value)}` : "未绑定",
    action: boundPhone.value ? "更换" : "绑定",
  },
  {
    key: "email" as Panel,
    icon: "ri-mail-line",
    title: "邮箱",
    sub: boundEmail.value ? `已绑定 ${maskEmail(boundEmail.value)}` : "未绑定",
    action: boundEmail.value ? "更换" : "绑定",
  },
]);

const panelTitle = computed(() => {
  switch (panel.value) {
    case "password":
      return hasPassword.value ? "修改密码" : "设置密码";
    case "phone":
      return boundPhone.value ? "更换手机号" : "绑定手机号";
    default:
      return boundEmail.value ? "更换邮箱" : "绑定邮箱";
  }
});

function resetAll() {
  pw.old = pw.next = pw.confirm = "";
  ph.oldCode = ph.next = ph.newCode = "";
  ph.sendingOld = ph.sendingNew = false;
  em.next = em.code = "";
  em.sending = false;
  clearOldCd();
  clearNewCd();
  clearEmailCd();
  busy.value = false;
}

const panelRef = ref<HTMLElement | null>(null);

/** 打开抽屉后聚焦第一个输入框（而非默认的关闭按钮）。 */
function focusFirstInput() {
  panelRef.value?.querySelector("input")?.focus();
}

function open(key: Panel) {
  resetAll();
  panel.value = key;
  show.value = true;
}

function close() {
  show.value = false;
}

function normalizePhoneInput(value: string) {
  return value.replace(/\D/g, "").slice(0, 11);
}

async function sendOldCode() {
  if (!boundPhone.value) return;
  ph.sendingOld = true;
  try {
    await client.sendSms(boundPhone.value, "change_phone");
    message.success("验证码已发送至当前手机号");
    startOldCd();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    ph.sendingOld = false;
  }
}

async function sendNewCode() {
  if (!phoneValid.value) {
    message.error("请输入正确的 11 位手机号");
    return;
  }
  ph.sendingNew = true;
  try {
    await client.sendSms(ph.next.trim(), "bind_phone");
    message.success("验证码已发送");
    startNewCd();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    ph.sendingNew = false;
  }
}

async function sendEmailCode() {
  if (!emailValid.value) {
    message.error("请输入正确的邮箱");
    return;
  }
  em.sending = true;
  try {
    await client.sendEmail(em.next.trim(), "bind_email");
    message.success("验证码已发送");
    startEmailCd();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    em.sending = false;
  }
}

async function refreshUser() {
  session.setUser(await client.me());
}

function submitPassword() {
  if (hasPassword.value && !pw.old) {
    message.error("请输入当前密码");
    return;
  }
  if (pw.next.length < 8 || pw.next.length > 72) {
    message.error("密码长度需为 8-72 位");
    return;
  }
  if (pw.next !== pw.confirm) {
    message.error("两次输入的密码不一致");
    return;
  }
  dialog.warning({
    title: "确认修改密码",
    content: "修改后其它设备将退出登录，需要用新密码重新登录。",
    positiveText: "确认修改",
    negativeText: "取消",
    onPositiveClick: async () => {
      busy.value = true;
      try {
        await client.changePassword(pw.old, pw.next);
        message.success("密码已修改");
        close();
      } catch (e: unknown) {
        message.error(errMessage(e));
      } finally {
        busy.value = false;
      }
    },
  });
}

function submitPhone() {
  const bound = !!boundPhone.value;
  if (bound && !ph.oldCode.trim()) {
    message.error("请输入原手机号验证码");
    return;
  }
  if (!phoneValid.value) {
    message.error("请输入正确的 11 位手机号");
    return;
  }
  if (!ph.newCode.trim()) {
    message.error("请输入新手机号验证码");
    return;
  }
  const next = ph.next.trim();
  dialog.warning({
    title: bound ? "确认更换手机号" : "确认绑定手机号",
    content: `手机号将变更为 ${maskPhone(next)}，之后可用它接收验证码登录。`,
    positiveText: "确认",
    negativeText: "取消",
    onPositiveClick: async () => {
      busy.value = true;
      try {
        await client.changePhone({
          old_phone: bound ? boundPhone.value : undefined,
          old_code: bound ? ph.oldCode.trim() : undefined,
          new_phone: next,
          new_code: ph.newCode.trim(),
        });
        await refreshUser();
        message.success(bound ? "手机号已更新" : "手机号已绑定");
        close();
      } catch (e: unknown) {
        message.error(errMessage(e));
      } finally {
        busy.value = false;
      }
    },
  });
}

function submitEmail() {
  if (!emailValid.value) {
    message.error("请输入正确的邮箱");
    return;
  }
  if (!em.code.trim()) {
    message.error("请输入验证码");
    return;
  }
  const next = em.next.trim();
  const bound = !!boundEmail.value;
  dialog.warning({
    title: bound ? "确认更换邮箱" : "确认绑定邮箱",
    content: `邮箱将变更为 ${next}，之后可用它登录和找回密码。`,
    positiveText: "确认",
    negativeText: "取消",
    onPositiveClick: async () => {
      busy.value = true;
      try {
        await client.bindEmail(next, em.code.trim());
        await refreshUser();
        message.success(bound ? "邮箱已更新" : "邮箱已绑定");
        close();
      } catch (e: unknown) {
        message.error(errMessage(e));
      } finally {
        busy.value = false;
      }
    },
  });
}

const isMobile = ref(typeof window !== "undefined" && window.innerWidth < 640);
function onResize() {
  isMobile.value = window.innerWidth < 640;
}
onMounted(() => window.addEventListener("resize", onResize));
onUnmounted(() => window.removeEventListener("resize", onResize));
</script>

<template>
  <div class="dauth-page security">
    <header class="head">
      <h1>安全</h1>
      <p>管理你的登录密码与账号凭据</p>
    </header>

    <div class="list">
      <div v-for="row in rows" :key="row.key" class="row">
        <i :class="`${row.icon} dauth-ri row-icon`" />
        <div class="row-main">
          <div class="row-title">{{ row.title }}</div>
          <div class="row-sub">{{ row.sub }}</div>
        </div>
        <n-button size="small" @click="open(row.key)">{{ row.action }}</n-button>
      </div>
    </div>

    <!-- auto-focus 默认会聚焦抽屉内第一个可聚焦元素（即头部关闭按钮），
         导致刚打开时关闭按钮呈现 active 态；改为手动聚焦第一个输入框。 -->
    <n-drawer
      v-model:show="show"
      :width="isMobile ? '100%' : 420"
      placement="right"
      :auto-focus="false"
      @after-enter="focusFirstInput"
      @after-leave="resetAll"
    >
      <n-drawer-content :title="panelTitle" closable>
        <div ref="panelRef">
        <n-form v-if="panel === 'password'" label-placement="top">
          <n-form-item v-if="hasPassword" label="当前密码">
            <n-input v-model:value="pw.old" type="password" show-password-on="click" placeholder="请输入当前密码" />
          </n-form-item>
          <n-form-item label="新密码">
            <n-input v-model:value="pw.next" type="password" show-password-on="click" placeholder="8-72 位" />
          </n-form-item>
          <n-form-item label="确认新密码">
            <n-input
              v-model:value="pw.confirm"
              type="password"
              show-password-on="click"
              placeholder="再次输入新密码"
              @keyup.enter="submitPassword"
            />
          </n-form-item>
          <n-button type="primary" block :loading="busy" @click="submitPassword">保存</n-button>
        </n-form>

        <n-form v-else-if="panel === 'phone'" label-placement="top">
          <n-form-item v-if="boundPhone" label="当前手机号">
            <n-input :value="maskPhone(boundPhone)" disabled />
          </n-form-item>
          <n-form-item v-if="boundPhone" label="原手机号验证码">
            <div class="dauth-code-row">
              <n-input v-model:value="ph.oldCode" maxlength="6" placeholder="请输入验证码" />
              <n-button
                :loading="ph.sendingOld"
                :disabled="ph.sendingOld || oldSeconds > 0"
                @click="sendOldCode"
              >
                {{ cdLabel(oldSeconds) }}
              </n-button>
            </div>
          </n-form-item>
          <n-form-item label="新手机号">
            <n-input
              :value="ph.next"
              maxlength="11"
              inputmode="numeric"
              placeholder="请输入新手机号"
              @update:value="ph.next = normalizePhoneInput($event)"
            />
          </n-form-item>
          <n-form-item label="新手机号验证码">
            <div class="dauth-code-row">
              <n-input v-model:value="ph.newCode" maxlength="6" placeholder="请输入验证码" />
              <n-button
                :loading="ph.sendingNew"
                :disabled="!phoneValid || ph.sendingNew || newSeconds > 0"
                @click="sendNewCode"
              >
                {{ cdLabel(newSeconds) }}
              </n-button>
            </div>
          </n-form-item>
          <n-button type="primary" block :loading="busy" @click="submitPhone">保存</n-button>
        </n-form>

        <n-form v-else label-placement="top">
          <n-form-item label="新邮箱">
            <n-input v-model:value="em.next" placeholder="请输入邮箱" @keyup.enter="submitEmail" />
          </n-form-item>
          <n-form-item label="验证码">
            <div class="dauth-code-row">
              <n-input v-model:value="em.code" maxlength="6" placeholder="请输入验证码" />
              <n-button
                :loading="em.sending"
                :disabled="!emailValid || em.sending || emailSeconds > 0"
                @click="sendEmailCode"
              >
                {{ cdLabel(emailSeconds) }}
              </n-button>
            </div>
          </n-form-item>
          <n-button type="primary" block :loading="busy" @click="submitEmail">保存</n-button>
        </n-form>
        </div>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>

<style scoped>
.head {
  margin-bottom: 20px;
}
.head h1 {
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 600;
}
.head p {
  margin: 0;
  font-size: 13px;
  color: var(--dauth-text-secondary);
}
.list {
  border-radius: 12px;
  background: var(--dauth-surface);
}
.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
}
.row + .row {
  border-top: 1px solid var(--dauth-border);
}
.row-icon {
  font-size: 18px;
  color: var(--dauth-text-secondary);
  flex-shrink: 0;
}
.row-main {
  flex: 1;
  min-width: 0;
}
.row-title {
  font-size: 14px;
  font-weight: 500;
}
.row-sub {
  margin-top: 2px;
  font-size: 12px;
  color: var(--dauth-text-secondary);
}
</style>
