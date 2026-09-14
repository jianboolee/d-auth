<script setup lang="ts">
import { reactive, ref } from "vue";
import { NButton, NCard, NForm, NFormItem, NInput, NSelect, useMessage } from "naive-ui";
import { useAdminClient } from "../client";

const client = useAdminClient();
const message = useMessage();
const form = reactive({ nickname: "", phone: "", email: "", password: "", roles: ["user"] });
const loading = ref(false);

async function submit() {
  loading.value = true;
  try {
    await client.adminCreateUser(form);
    message.success("用户已创建");
    form.nickname = "";
    form.phone = "";
    form.email = "";
    form.password = "";
  } catch (e: any) {
    message.error(e.message ?? "创建失败");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <n-card title="创建用户" class="d-auth-admin" style="max-width: 560px">
    <n-form label-placement="left" label-width="80">
      <n-form-item label="昵称"><n-input v-model:value="form.nickname" /></n-form-item>
      <n-form-item label="手机"><n-input v-model:value="form.phone" /></n-form-item>
      <n-form-item label="邮箱"><n-input v-model:value="form.email" /></n-form-item>
      <n-form-item label="密码"><n-input v-model:value="form.password" type="password" /></n-form-item>
      <n-form-item label="角色">
        <n-select v-model:value="form.roles" multiple :options="[
          {label:'user',value:'user'},
          {label:'admin',value:'admin'},
          {label:'superadmin',value:'superadmin'},
        ]" />
      </n-form-item>
      <n-button type="primary" :loading="loading" @click="submit">创建</n-button>
    </n-form>
  </n-card>
</template>
