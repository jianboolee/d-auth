<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import type { CSSProperties } from "vue";
import { NButton, NCard, NDataTable, NTooltip, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Session } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../../client";
import { useConfirm } from "../../confirm";
import { formatDateTime } from "../../datetime";
import { errMessage } from "../../labels";
import { useTableScrollX } from "../../viewport";

const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const tableScrollX = useTableScrollX();
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

/**
 * 设备单元格：图标与文字水平对齐，并撑开 8px 间距。
 * 图标不设颜色，靠 theme.css 的 `.dauth-ri { color: inherit }` 继承同格的
 * 文字色，天然与文字一致（写死颜色反而会和表格的文字色差一档）。
 */
const cellStyles: CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "8px",
};

const columns: DataTableColumns<Session> = [
  {
    title: "设备",
    key: "device",
    // 单元格内容由 render 函数生成，不经过模板编译，拿不到 scoped 的 data-v
    // 属性——写在该组件 <style scoped> 里的规则会静默失效。布局只能用内联样式。
    render: (r) =>
      h("div", { class: "device-cell", style: cellStyles }, [
        h("i", { class: "ri-computer-line dauth-ri" }),
        // 原始 UA 仅用于排查，收起在提示里。放在设备名之前：
        // 跟在名字后面时，图标位置会随名字长短左右浮动。
        r.user_agent
          ? h(
              NTooltip,
              { trigger: "hover", style: { maxWidth: "420px" } },
              {
                trigger: () =>
                  h("i", {
                    class: "ri-information-line dauth-ri device-info",
                    style: { cursor: "help" },
                  }),
                default: () => r.user_agent,
              },
            )
          : null,
        h("span", null, r.device),
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
  <n-card class="dauth-page wide">
    <n-data-table :columns="columns" :data="items" :loading="loading" :scroll-x="tableScrollX">
      <template #empty>
        <div class="dauth-empty">
          <i class="ri-inbox-2-line dauth-ri" />
          暂无数据
        </div>
      </template>
    </n-data-table>
  </n-card>
</template>
