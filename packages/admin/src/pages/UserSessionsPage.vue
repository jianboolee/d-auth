<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import { NButton, NCard, NDataTable, NSpace, NTag, NTooltip, useMessage } from "naive-ui";
import type { CSSProperties } from "vue";
import type { DataTableColumns, PaginationProps } from "naive-ui";
import { useRoute, useRouter } from "vue-router";
import type { Session } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { formatDateTime } from "../datetime";
import { errMessage } from "../labels";
import { useTableScrollX } from "../viewport";

const props = defineProps<{ basePath?: string }>();
const route = useRoute();
const router = useRouter();
const client = useAdminClient();
const message = useMessage();
const tableScrollX = useTableScrollX();
const items = ref<Session[]>([]);
const total = ref(0);
const loading = ref(false);
const q = reactive({ page: 1, size: 10 });

const userId = computed(() => String(route.params.id));

async function load() {
  loading.value = true;
  try {
    const data = await client.adminUserSessions(userId.value, { page: q.page, size: q.size });
    items.value = data.items;
    total.value = data.total;
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

function back() {
  router.push(`${props.basePath ?? "/admin"}/users/${userId.value}`);
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

/** 会话状态：已下线 > 已过期 > 有效。后台需要区分，便于排查「为什么被踢」。 */
function sessionStatus(s: Session): { label: string; type: "success" | "warning" | "error" } {
  if (s.revoked_at) return { label: "已下线", type: "error" };
  if (new Date(s.expires_at).getTime() <= Date.now()) return { label: "已过期", type: "warning" };
  return { label: "有效", type: "success" };
}

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
  {
    title: "状态",
    key: "status",
    render: (r) => {
      const s = sessionStatus(r);
      return h(NTag, { size: "small", type: s.type }, { default: () => s.label });
    },
  },
  { title: "登录时间", key: "created_at", render: (r) => formatDateTime(r.created_at) },
  { title: "过期时间", key: "expires_at", render: (r) => formatDateTime(r.expires_at) },
  { title: "下线时间", key: "revoked_at", render: (r) => (r.revoked_at ? formatDateTime(r.revoked_at) : "—") },
];

/**
 * 分页交给服务端。必须显式给 remote（见模板）：否则 naive 会忽略 itemCount，
 * 改用当前这一页数组的长度当总数，翻不到第 2 页，还会把已分页的数组再本地
 * 切一次导致空白页。
 */
const pagination = computed<PaginationProps>(() => ({
  page: q.page,
  pageSize: q.size,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  prefix: ({ itemCount }) => `共 ${itemCount} 条`,
  onChange: (page: number) => {
    q.page = page;
    load();
  },
  onUpdatePageSize: (size: number) => {
    q.size = size;
    // 换每页条数后原页码可能越界，回到第一页
    q.page = 1;
    load();
  },
}));

onMounted(load);
</script>

<template>
  <div class="d-auth-admin dauth-page wide">
    <n-space vertical size="large">
      <n-button text @click="back">
        <template #icon><i class="ri-arrow-left-line dauth-ri" /></template>
        返回用户详情
      </n-button>
      <n-card title="会话记录">
        <p class="hint">这里包含已失效与被下线的历史会话，用于排查登录情况。</p>
        <n-data-table
          remote
          :columns="columns"
          :data="items"
          :loading="loading"
          :pagination="pagination"
          :scroll-x="tableScrollX"
        >
          <template #empty>
            <div class="dauth-empty">
              <i class="ri-inbox-2-line dauth-ri" />
              暂无数据
            </div>
          </template>
        </n-data-table>
      </n-card>
    </n-space>
  </div>
</template>

<style scoped>
.hint {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--dauth-text-secondary, #8c8c8c);
}
</style>
