<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NCard, NDataTable } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Role } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const client = useAdminClient();
const items = ref<Role[]>([]);
const columns: DataTableColumns<Role> = [
  { title: "代码", key: "code" },
  { title: "名称", key: "name" },
  { title: "说明", key: "description" },
  { title: "系统角色", key: "is_system", render: (r) => (r.is_system ? "是" : "否") },
];

onMounted(async () => {
  items.value = await client.adminRoles();
});
</script>

<template>
  <n-card title="角色" class="d-auth-admin dauth-page wide">
    <n-data-table :columns="columns" :data="items">
      <template #empty>
        <div class="dauth-empty"><i class="ri-inbox-2-line dauth-ri" />暂无数据</div>
      </template>
    </n-data-table>
  </n-card>
</template>
