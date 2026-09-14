<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NButton, NCard, NDataTable, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Session } from "@donktech/d-auth-sdk";
import { h } from "vue";
import { dauth } from "../../api/client";

const message = useMessage();
const items = ref<Session[]>([]);

async function load() {
  items.value = await dauth.sessions();
}

async function revoke(id: string) {
  await dauth.deleteSession(id);
  message.success("已下线");
  await load();
}

const columns: DataTableColumns<Session> = [
  { title: "IP", key: "ip" },
  { title: "设备", key: "user_agent" },
  { title: "过期", key: "expires_at" },
  {
    title: "操作",
    key: "op",
    render: (r) => h(NButton, { text: true, type: "error", onClick: () => revoke(r.id) }, { default: () => "下线" }),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="登录会话">
    <n-data-table :columns="columns" :data="items" />
  </n-card>
</template>
