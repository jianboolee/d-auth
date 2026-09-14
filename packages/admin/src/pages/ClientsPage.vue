<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NSpace, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ClientApp } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const client = useAdminClient();
const message = useMessage();
const items = ref<ClientApp[]>([]);
const show = ref(false);
const form = reactive({
  client_key: "",
  name: "",
  type: "miniprogram",
  allowed_origins: "",
  login_methods: "wechat_mini",
  wechat_app_id: "",
  wechat_app_secret: "",
});

const columns: DataTableColumns<ClientApp> = [
  { title: "Key", key: "client_key" },
  { title: "名称", key: "name" },
  { title: "类型", key: "type" },
  { title: "微信 AppID", key: "wechat_app_id" },
  { title: "状态", key: "status" },
];

async function load() {
  items.value = await client.adminClients();
}

async function create() {
  try {
    await client.adminCreateClient({
      client_key: form.client_key,
      name: form.name,
      type: form.type,
      allowed_origins: form.allowed_origins.split(",").map((s) => s.trim()).filter(Boolean),
      login_methods: form.login_methods.split(",").map((s) => s.trim()).filter(Boolean),
      wechat_app_id: form.wechat_app_id,
      wechat_app_secret: form.wechat_app_secret,
    });
    show.value = false;
    message.success("已创建");
    await load();
  } catch (e: any) {
    message.error(e.message ?? "创建失败");
  }
}

onMounted(load);
</script>

<template>
  <n-card title="应用 / 小程序" class="d-auth-admin">
    <template #header-extra>
      <n-button type="primary" @click="show = true">新建</n-button>
    </template>
    <n-data-table :columns="columns" :data="items" />
    <n-modal v-model:show="show" preset="card" title="新建应用" style="width: 520px">
      <n-form label-placement="left" label-width="110">
        <n-form-item label="client_key"><n-input v-model:value="form.client_key" /></n-form-item>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="类型">
          <n-select v-model:value="form.type" :options="[{label:'web',value:'web'},{label:'miniprogram',value:'miniprogram'},{label:'native',value:'native'}]" />
        </n-form-item>
        <n-form-item label="回调 Origin"><n-input v-model:value="form.allowed_origins" placeholder="逗号分隔" /></n-form-item>
        <n-form-item label="登录方式"><n-input v-model:value="form.login_methods" placeholder="sms,email,wechat_qr,wechat_mini" /></n-form-item>
        <n-form-item label="微信 AppID"><n-input v-model:value="form.wechat_app_id" /></n-form-item>
        <n-form-item label="微信 Secret"><n-input v-model:value="form.wechat_app_secret" type="password" /></n-form-item>
      </n-form>
      <n-space justify="end"><n-button type="primary" @click="create">保存</n-button></n-space>
    </n-modal>
  </n-card>
</template>
