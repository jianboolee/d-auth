<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSpace, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ApiKey } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const client = useAdminClient();
const message = useMessage();
const items = ref<ApiKey[]>([]);
const show = ref(false);
const secret = ref("");
const form = reactive({ name: "", scopes: "users:read,users:write", days: "0" });

const columns: DataTableColumns<ApiKey> = [
  { title: "名称", key: "name" },
  { title: "前缀", key: "key_prefix" },
  { title: "scopes", key: "scopes", render: (r) => r.scopes.join(", ") },
  { title: "状态", key: "status" },
];

async function load() {
  items.value = await client.adminApiKeys();
}

async function create() {
  try {
    const res = await client.adminCreateApiKey({
      name: form.name,
      scopes: form.scopes.split(",").map((s) => s.trim()).filter(Boolean),
      days: Number(form.days) || 0,
    });
    secret.value = res.secret;
    await load();
    message.success("请立即复制密钥，之后无法再查看");
  } catch (e: any) {
    message.error(e.message ?? "创建失败");
  }
}

onMounted(load);
</script>

<template>
  <n-card title="API Key" class="d-auth-admin">
    <template #header-extra>
      <n-button type="primary" @click="show = true">新建</n-button>
    </template>
    <n-data-table :columns="columns" :data="items" />
    <n-modal v-model:show="show" preset="card" title="新建 API Key" style="width: 520px" @after-leave="secret = ''">
      <n-form label-placement="left" label-width="80">
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="scopes"><n-input v-model:value="form.scopes" /></n-form-item>
        <n-form-item label="有效天数"><n-input v-model:value="form.days" placeholder="0 表示不过期" /></n-form-item>
      </n-form>
      <n-space vertical>
        <n-button type="primary" @click="create">创建</n-button>
        <n-input v-if="secret" :value="secret" readonly type="textarea" />
      </n-space>
    </n-modal>
  </n-card>
</template>
