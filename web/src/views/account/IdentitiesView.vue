<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { NButton, NCard, NDataTable, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Identity } from "@donktech/d-auth-sdk";
import { dauth } from "../../api/client";

const message = useMessage();
const items = ref<Identity[]>([]);

async function load() {
  items.value = await dauth.identities();
}

async function unbind(id: string) {
  try {
    await dauth.deleteIdentity(id);
    message.success("已解绑");
    await load();
  } catch (e: any) {
    message.error(e.message);
  }
}

const columns: DataTableColumns<Identity> = [
  { title: "类型", key: "type" },
  { title: "标识", key: "identifier" },
  {
    title: "操作",
    key: "op",
    render: (r) => h(NButton, { text: true, type: "error", onClick: () => unbind(r.id) }, { default: () => "解绑" }),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="登录方式">
    <n-data-table :columns="columns" :data="items" />
  </n-card>
</template>
