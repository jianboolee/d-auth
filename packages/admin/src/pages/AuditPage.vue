<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { NCard, NDataTable, useMessage } from "naive-ui";
import type { DataTableColumns, PaginationProps } from "naive-ui";
import type { AuditLog } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { formatDateTime } from "../datetime";
import { errMessage } from "../labels";

const client = useAdminClient();
const message = useMessage();
const items = ref<AuditLog[]>([]);
const loading = ref(false);
const total = ref(0);
const q = reactive({ page: 1, size: 20 });

const columns: DataTableColumns<AuditLog> = [
  // 后端返回的是带纳秒的 RFC3339，直接显示既长又难读；与其它表格统一格式化
  { title: "时间", key: "created_at", render: (r) => formatDateTime(r.created_at) },
  { title: "动作", key: "action" },
  { title: "操作者", key: "actor_user_id", render: (r) => r.actor_user_id || "—" },
  { title: "对象", key: "target_user_id", render: (r) => r.target_user_id || "—" },
  { title: "IP 地址", key: "ip", render: (r) => r.ip || "—" },
];

async function load() {
  loading.value = true;
  try {
    const data = await client.adminAudit({ page: q.page, size: q.size });
    items.value = data.items;
    total.value = data.total;
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

/**
 * 分页交给服务端：这里每次只拿到一页数据。
 *
 * 必须同时给 remote 和 itemCount —— 否则 naive 会拿当前这一页的数组在本地
 * 再切一次，第 2 页会渲染成空白。改动前这个页面固定取最新 50 条且没有分页
 * 控件，更早的记录在界面上根本看不到。
 */
const pagination = computed<PaginationProps>(() => ({
  page: q.page,
  pageSize: q.size,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  prefix: ({ itemCount }) => `共 ${itemCount} 条`,
  onChange: (page: number) => {
    q.page = page;
    load();
  },
  onUpdatePageSize: (size: number) => {
    q.size = size;
    // 换每页条数后原来的页码可能越界，回到第一页
    q.page = 1;
    load();
  },
}));

onMounted(load);
</script>

<template>
  <n-card title="审计日志" class="d-auth-admin dauth-page wide">
    <n-data-table
      remote
      :columns="columns"
      :data="items"
      :loading="loading"
      :pagination="pagination"
    >
      <template #empty>
        <div class="dauth-empty"><i class="ri-inbox-2-line dauth-ri" />暂无数据</div>
      </template>
    </n-data-table>
  </n-card>
</template>
