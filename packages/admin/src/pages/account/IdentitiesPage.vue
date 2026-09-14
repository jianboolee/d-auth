<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { NButton, NCard, NDataTable, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Identity } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../../client";
import { errMessage, identityLabel } from "../../labels";

const client = useAdminClient();
const message = useMessage();
const items = ref<Identity[]>([]);

async function load() {
  items.value = await client.identities();
}

async function unbind(id: string) {
  try {
    await client.deleteIdentity(id);
    message.success("已解绑");
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}

const columns: DataTableColumns<Identity> = [
  { title: "类型", key: "type", render: (r) => identityLabel(r.type) },
  { title: "标识", key: "identifier" },
  {
    title: "操作",
    key: "op",
    render: (r) =>
      h(
        NButton,
        { text: true, type: "error", onClick: () => unbind(r.id) },
        {
          icon: () => h("i", { class: "ri-link-unlink-m dauth-ri" }),
          default: () => "解绑",
        },
      ),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="登录方式" class="dauth-page wide">
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
