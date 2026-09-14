<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NSpace, NTag, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ApiKey } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { errMessage, scopeOptions, statusLabel } from "../labels";

const client = useAdminClient();
const message = useMessage();
const items = ref<ApiKey[]>([]);
const show = ref(false);
const secret = ref("");
const form = reactive({ name: "", scopes: ["users:read", "users:write"] as string[], days: "0" });

const columns: DataTableColumns<ApiKey> = [
  { title: "名称", key: "name" },
  { title: "前缀", key: "key_prefix" },
  { title: "权限范围", key: "scopes", render: (r) => r.scopes.join("、") },
  {
    title: "状态",
    key: "status",
    render: (r) => h(NTag, { size: "small", type: r.status === "active" ? "success" : "warning" }, { default: () => statusLabel(r.status) }),
  },
];

async function load() {
  items.value = await client.adminApiKeys();
}

async function create() {
  try {
    const res = await client.adminCreateApiKey({
      name: form.name,
      scopes: form.scopes,
      days: Number(form.days) || 0,
    });
    secret.value = res.secret;
    await load();
    message.success("请立即复制密钥，之后无法再查看");
  } catch (e: unknown) {
    message.error(errMessage(e, "创建失败"));
  }
}

onMounted(load);
</script>

<template>
  <n-card title="接口密钥" class="d-auth-admin dauth-page wide">
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
    <n-modal v-model:show="show" preset="card" title="新建接口密钥" style="width: 520px" @after-leave="secret = ''">
      <n-form label-placement="top">
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="权限范围">
          <n-select v-model:value="form.scopes" multiple :options="scopeOptions" />
        </n-form-item>
        <n-form-item label="有效天数"><n-input v-model:value="form.days" placeholder="0 表示不过期" /></n-form-item>
      </n-form>
      <n-space vertical style="margin-top: 12px">
        <n-button type="primary" @click="create">创建</n-button>
        <n-input v-if="secret" :value="secret" readonly type="textarea" />
      </n-space>
    </n-modal>
  </n-card>
</template>
