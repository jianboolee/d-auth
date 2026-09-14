<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NSpace, NTag, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ClientApp } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { clientTypeLabel, clientTypeOptions, errMessage, loginMethodOptions, statusLabel } from "../labels";

const client = useAdminClient();
const message = useMessage();
const items = ref<ClientApp[]>([]);
const show = ref(false);
const form = reactive({
  client_key: "",
  name: "",
  type: "miniprogram",
  allowed_origins: "",
  login_methods: ["wechat_mini"] as string[],
  wechat_app_id: "",
  wechat_app_secret: "",
});

const columns: DataTableColumns<ClientApp> = [
  { title: "应用标识", key: "client_key" },
  { title: "名称", key: "name" },
  { title: "类型", key: "type", render: (r) => clientTypeLabel(r.type) },
  { title: "微信应用编号", key: "wechat_app_id" },
  {
    title: "状态",
    key: "status",
    render: (r) => h(NTag, { size: "small", type: r.status === "active" ? "success" : "warning" }, { default: () => statusLabel(r.status) }),
  },
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
      login_methods: form.login_methods,
      wechat_app_id: form.wechat_app_id,
      wechat_app_secret: form.wechat_app_secret,
    });
    show.value = false;
    message.success("已创建");
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e, "创建失败"));
  }
}

onMounted(load);
</script>

<template>
  <n-card title="应用 / 小程序" class="d-auth-admin dauth-page wide">
    <template #header-extra>
      <n-button type="primary" @click="show = true">
        <template #icon><i class="ri-add-line dauth-ri" /></template>
        新建
      </n-button>
    </template>
    <n-data-table :columns="columns" :data="items">
      <template #empty>
        <div class="dauth-empty"><i class="ri-inbox-2-line dauth-ri" />暂无数据</div>
      </template>
    </n-data-table>
    <n-modal v-model:show="show" preset="card" title="新建应用" style="width: 520px">
      <n-form label-placement="top">
        <n-form-item label="应用标识"><n-input v-model:value="form.client_key" placeholder="例如 web_ticket" /></n-form-item>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="类型">
          <n-select v-model:value="form.type" :options="clientTypeOptions" />
        </n-form-item>
        <n-form-item label="允许的来源地址">
          <n-input v-model:value="form.allowed_origins" placeholder="多个地址用逗号分隔" />
        </n-form-item>
        <n-form-item label="登录方式">
          <n-select v-model:value="form.login_methods" multiple :options="loginMethodOptions" />
        </n-form-item>
        <n-form-item label="微信应用编号"><n-input v-model:value="form.wechat_app_id" /></n-form-item>
        <n-form-item label="微信密钥"><n-input v-model:value="form.wechat_app_secret" type="password" /></n-form-item>
      </n-form>
      <n-space justify="end" style="margin-top: 12px">
        <n-button type="primary" @click="create">保存</n-button>
      </n-space>
    </n-modal>
  </n-card>
</template>
