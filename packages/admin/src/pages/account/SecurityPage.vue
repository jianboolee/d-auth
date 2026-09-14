<script setup lang="ts">
import { ref } from "vue";
import { NCard, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { errMessage } from "../../labels";

const client = useAdminClient();
const session = useAuthSession();
const { user } = session;
const message = useMessage();
const oldPass = ref("");
const newPass = ref("");
const oldPhone = ref(user.value?.phone ?? "");
const oldCode = ref("");
const newPhone = ref("");
const newCode = ref("");
const email = ref("");
const emailCode = ref("");

async function changePass() {
  try {
    await client.changePassword(oldPass.value, newPass.value);
    message.success("密码已修改");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}

async function sendOld() {
  try {
    await client.sendSms(oldPhone.value || user.value?.phone || "", "change_phone");
    message.success("已发送");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
async function sendNew() {
  try {
    await client.sendSms(newPhone.value, "bind_phone");
    message.success("已发送");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
async function changePhone() {
  try {
    await client.changePhone({
      old_phone: oldPhone.value,
      old_code: oldCode.value,
      new_phone: newPhone.value,
      new_code: newCode.value,
    });
    session.setUser(await client.me());
    message.success("手机号已更新");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
async function sendEmail() {
  try {
    await client.sendEmail(email.value, "bind_email");
    message.success("已发送");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
async function bindEmail() {
  try {
    await client.bindEmail(email.value, emailCode.value);
    session.setUser(await client.me());
    message.success("邮箱已绑定");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}
</script>

<template>
  <n-space vertical size="large" class="dauth-page">
    <n-card title="修改密码">
      <n-form label-placement="top">
        <n-form-item label="原密码"><n-input v-model:value="oldPass" type="password" /></n-form-item>
        <n-form-item label="新密码"><n-input v-model:value="newPass" type="password" placeholder="至少 8 位" /></n-form-item>
        <n-button type="primary" @click="changePass">更新密码</n-button>
      </n-form>
    </n-card>
    <n-card title="修改手机号">
      <n-form label-placement="top">
        <n-form-item label="原手机号"><n-input v-model:value="oldPhone" /></n-form-item>
        <n-form-item label="原号验证码">
          <div class="dauth-code-row">
            <n-input v-model:value="oldCode" placeholder="验证码" />
            <n-button @click="sendOld">
              <template #icon><i class="ri-send-plane-line dauth-ri" /></template>
              发送
            </n-button>
          </div>
        </n-form-item>
        <n-form-item label="新手机号"><n-input v-model:value="newPhone" /></n-form-item>
        <n-form-item label="新号验证码">
          <div class="dauth-code-row">
            <n-input v-model:value="newCode" placeholder="验证码" />
            <n-button @click="sendNew">
              <template #icon><i class="ri-send-plane-line dauth-ri" /></template>
              发送
            </n-button>
          </div>
        </n-form-item>
        <n-button type="primary" @click="changePhone">更换手机号</n-button>
      </n-form>
    </n-card>
    <n-card title="绑定邮箱">
      <n-form label-placement="top">
        <n-form-item label="邮箱"><n-input v-model:value="email" /></n-form-item>
        <n-form-item label="验证码">
          <div class="dauth-code-row">
            <n-input v-model:value="emailCode" placeholder="验证码" />
            <n-button @click="sendEmail">
              <template #icon><i class="ri-send-plane-line dauth-ri" /></template>
              发送
            </n-button>
          </div>
        </n-form-item>
        <n-button type="primary" @click="bindEmail">绑定</n-button>
      </n-form>
    </n-card>
  </n-space>
</template>
