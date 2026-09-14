<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NInput, NSelect, NSpace, NTag, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { useRouter } from "vue-router";
import type { User } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const props = defineProps<{ basePath?: string }>();
const client = useAdminClient();
const router = useRouter();
const message = useMessage();
const loading = ref(false);
const items = ref<User[]>([]);
const total = ref(0);
const q = reactive({ q: "", status: "", role: "", page: 1, size: 20 });

const statusOptions = [
  { label: "全部状态", value: "" },
  { label: "正常", value: "active" },
  { label: "停用", value: "disabled" },
];

async function load() {
  loading.value = true;
  try {
    const data = await client.adminUsers(q);
    items.value = data.items;
    total.value = data.total;
  } catch (e: any) {
    message.error(e.message ?? "加载失败");
  } finally {
    loading.value = false;
  }
}

function open(id: string) {
  const base = props.basePath ?? "/admin";
  router.push(`${base}/users/${id}`);
}

const columns: DataTableColumns<User> = [
  { title: "昵称", key: "nickname" },
  { title: "手机", key: "phone", render: (r) => r.phone || "-" },
  { title: "邮箱", key: "email", render: (r) => r.email || "-" },
  {
    title: "角色",
    key: "roles",
    render: (r) => h(NSpace, { size: 4 }, () => r.roles.map((role) => h(NTag, { size: "small", bordered: false }, { default: () => role }))),
  },
  {
    title: "状态",
    key: "status",
    render: (r) => h(NTag, { type: r.status === "active" ? "success" : "warning", size: "small" }, { default: () => r.status }),
  },
  {
    title: "操作",
    key: "actions",
    render: (r) => h(NButton, { text: true, type: "primary", onClick: () => open(r.id) }, { default: () => "详情" }),
  },
];

onMounted(load);
</script>

<template>
  <div class="d-auth-admin">
    <n-card title="用户管理">
      <n-space style="margin-bottom: 16px" align="center">
        <n-input v-model:value="q.q" placeholder="搜索手机 / 邮箱 / 昵称" style="width: 240px" @keyup.enter="load" />
        <n-select v-model:value="q.status" :options="statusOptions" style="width: 140px" />
        <n-button type="primary" :loading="loading" @click="load">查询</n-button>
      </n-space>
      <n-data-table
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="{ page: q.page, pageSize: q.size, itemCount: total, onChange: (p: number) => { q.page = p; load(); } }"
      />
    </n-card>
  </div>
</template>
