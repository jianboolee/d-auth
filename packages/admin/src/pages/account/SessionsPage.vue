<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { NButton, NCard, NDataTable, NTooltip, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Session } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../../client";
import { useConfirm } from "../../confirm";
import { formatDateTime } from "../../datetime";
import { errMessage } from "../../labels";

const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const items = ref<Session[]>([]);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    items.value = await client.sessions();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}

function confirmRevoke(row: Session) {
  confirm({
    title: "确认下线",
    content: `将中止「${row.device}」上的登录，该设备需要重新登录。`,
    positiveText: "下线",
    onPositiveClick: async () => {
      try {
        await client.deleteSession(row.id);
        message.success("已下线");
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e));
      }
    },
  });
}

const columns: DataTableColumns<Session> = [
  {
    title: "设备",
    key: "device",
    render: (r) =>
      h("div", { class: "device-cell" }, [
        h("i", { class: "ri-computer-line dauth-ri" }),
        h("span", null, r.device),
        // 原始 UA 仅用于排查，收起在提示里
        r.user_agent
          ? h(
              NTooltip,
              { trigger: "hover", style: { maxWidth: "420px" } },
              {
                trigger: () => h("i", { class: "ri-information-line dauth-ri device-info" }),
                default: () => r.user_agent,
              },
            )
          : null,
      ]),
  },
  { title: "IP 地址", key: "ip", render: (r) => r.ip || "—" },
  { title: "登录时间", key: "created_at", render: (r) => formatDateTime(r.created_at) },
  { title: "过期时间", key: "expires_at", render: (r) => formatDateTime(r.expires_at) },
  {
    title: "操作",
    key: "op",
    render: (r) =>
      h(
        NButton,
        { text: true, type: "error", onClick: () => confirmRevoke(r) },
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
  <n-card title="登录设备" class="dauth-page wide">
    <n-data-table :columns="columns" :data="items" :loading="loading">
      <template #empty>
        <div class="dauth-empty">
          <i class="ri-inbox-2-line dauth-ri" />
          暂无数据
        </div>
      </template>
    </n-data-table>
  </n-card>
</template>

<style scoped>
.device-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}
.device-cell .dauth-ri {
  color: var(--dauth-text-secondary);
}
.device-info {
  cursor: help;
  font-size: 14px;
}
</style>
