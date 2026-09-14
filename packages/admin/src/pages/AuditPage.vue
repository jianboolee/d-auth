<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NCard, NDataTable } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { AuditLog } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const client = useAdminClient();
const items = ref<AuditLog[]>([]);
const columns: DataTableColumns<AuditLog> = [
  { title: "时间", key: "created_at" },
  { title: "动作", key: "action" },
  { title: "操作者", key: "actor_user_id" },
  { title: "对象", key: "target_user_id" },
  { title: "IP", key: "ip" },
];

onMounted(async () => {
  const data = await client.adminAudit({ size: 50 });
  items.value = data.items;
});
</script>

<template>
  <n-card title="审计日志" class="d-auth-admin">
    <n-data-table :columns="columns" :data="items" />
  </n-card>
</template>
