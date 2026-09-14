<script setup lang="ts">
import { ref } from "vue";
import { NCard, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from "naive-ui";
import { dauth } from "../../api/client";
import { useAuthStore } from "../../stores/auth";

const auth = useAuthStore();
const message = useMessage();
const oldPass = ref("");
const newPass = ref("");
const oldPhone = ref(auth.user?.phone ?? "");
const oldCode = ref("");
const newPhone = ref("");
const newCode = ref("");
const email = ref("");
const emailCode = ref("");

async function changePass() {
  try {
    await dauth.changePassword(oldPass.value, newPass.value);
    message.success("密码已修改");
  } catch (e: any) {
    message.error(e.message);
  }
}

async function sendOld() {
  const res = await dauth.sendSms(oldPhone.value || (auth.user?.phone ?? ""), "change_phone");
  message.success(res.debug_code ? `验证码 ${res.debug_code}` : "已发送");
}
async function sendNew() {
  const res = await dauth.sendSms(newPhone.value, "bind_phone");
  message.success(res.debug_code ? `验证码 ${res.debug_code}` : "已发送");
}
async function changePhone() {
  try {
    await dauth.changePhone({
      old_phone: oldPhone.value,
      old_code: oldCode.value,
      new_phone: newPhone.value,
      new_code: newCode.value,
    });
    auth.user = await dauth.me();
    message.success("手机号已更新");
  } catch (e: any) {
    message.error(e.message);
  }
}
async function sendEmail() {
  const res = await dauth.sendEmail(email.value, "bind_email");
  message.success(res.debug_code ? `验证码 ${res.debug_code}` : "已发送");
}
async function bindEmail() {
  try {
    await dauth.bindEmail(email.value, emailCode.value);
    auth.user = await dauth.me();
    message.success("邮箱已绑定");
  } catch (e: any) {
    message.error(e.message);
  }
}
</script>

<template>
  <n-space vertical size="large" style="max-width: 640px">
    <n-card title="修改密码">
      <n-form>
        <n-form-item label="原密码"><n-input v-model:value="oldPass" type="password" /></n-form-item>
        <n-form-item label="新密码"><n-input v-model:value="newPass" type="password" /></n-form-item>
        <n-button type="primary" @click="changePass">更新密码</n-button>
      </n-form>
    </n-card>
    <n-card title="修改手机号">
      <n-form>
        <n-form-item label="原手机">
          <n-space>
            <n-input v-model:value="oldPhone" />
            <n-button @click="sendOld">发码</n-button>
            <n-input v-model:value="oldCode" placeholder="原号验证码" />
          </n-space>
        </n-form-item>
        <n-form-item label="新手机">
          <n-space>
            <n-input v-model:value="newPhone" />
            <n-button @click="sendNew">发码</n-button>
            <n-input v-model:value="newCode" placeholder="新号验证码" />
          </n-space>
        </n-form-item>
        <n-button type="primary" @click="changePhone">更换手机号</n-button>
      </n-form>
    </n-card>
    <n-card title="绑定邮箱">
      <n-form>
        <n-form-item label="邮箱">
          <n-space>
            <n-input v-model:value="email" />
            <n-button @click="sendEmail">发码</n-button>
            <n-input v-model:value="emailCode" placeholder="验证码" />
          </n-space>
        </n-form-item>
        <n-button type="primary" @click="bindEmail">绑定</n-button>
      </n-form>
    </n-card>
  </n-space>
</template>
