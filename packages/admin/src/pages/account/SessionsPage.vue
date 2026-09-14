<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { NButton, NCard, NDataTable, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Session } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../../client";
import { errMessage } from "../../labels";

const client = useAdminClient();
const message = useMessage();
const items = ref<Session[]>([]);

async function load() {
  items.value = await client.sessions();
}

async function revoke(id: string) {
  try {
    await client.deleteSession(id);
    message.success("已下线");
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}

const columns: DataTableColumns<Session> = [
  { title: "IP 地址", key: "ip" },
  { title: "设备", key: "user_agent" },
  { title: "过期", key: "expires_at" },
  {
    title: "操作",
    key: "op",
    render: (r) =>
      h(
        NButton,
        { text: true, type: "error", onClick: () => revoke(r.id) },
        {
          icon: () => h("i", { class: "ri-logout-box-r-line dauth-ri" }),
          default: () => "下线",
        },
      ),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="登录会话" class="dauth-page wide">
    <n-data-table :columns="columns" :data="items">
      <template #empty>
        <div class="dauth-empty">
          <i class="ri-inbox-2-line dauth-ri" />
          暂无数据
        </div>
      </template>
    </n-data-table>
  </n-card>
</template>
