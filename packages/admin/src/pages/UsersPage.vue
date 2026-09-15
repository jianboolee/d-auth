<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import {
  NButton,
  NCard,
  NDataTable,
  NDatePicker,
  NInput,
  NSelect,
  NSpace,
  NTag,
  useMessage,
} from "naive-ui";
import type { DataTableColumns, DataTableSortState, PaginationProps } from "naive-ui";
import { useRouter } from "vue-router";
import type { User } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { formatDateTime } from "../datetime";
import { errMessage, roleLabel, statusLabel } from "../labels";

const props = defineProps<{ basePath?: string }>();
const client = useAdminClient();
const router = useRouter();
const message = useMessage();
const loading = ref(false);
const items = ref<User[]>([]);
const total = ref(0);
const q = reactive({ q: "", status: "", role: "", page: 1, size: 10 });
/** 注册时间筛选，[开始日 0 点, 结束日 0 点] 的本地时间戳；null 表示不限。 */
const range = ref<[number, number] | null>(null);

type SortKey = "created_at" | "last_login_at" | "nickname";
/** 排序由服务端做，这里只保存「当前按什么排」，用受控 sortOrder 驱动表头箭头。 */
const sort = ref<{ columnKey: SortKey; order: "ascend" | "descend" }>({
  columnKey: "created_at",
  order: "descend",
});

const statusOptions = [
  { label: "全部状态", value: "" },
  { label: "正常", value: "active" },
  { label: "停用", value: "disabled" },
];

/**
 * 把日期区间换算成给后端的半开区间 [from, to)。
 *
 * naive 的 daterange 两端都返回当天 0 点的本地时间戳，右端 +1 天才能把
 * 「结束当天」整天包含进来；再转成 ISO，时区信息随之带上，服务端按
 * timestamptz 比较即得到用户本地日历的语义，不用在两边各自猜时区。
 */
function createdRange(): { from?: string; to?: string } {
  if (!range.value) return {};
  const [start, end] = range.value;
  const from = new Date(start);
  const to = new Date(end);
  to.setDate(to.getDate() + 1);
  return { from: from.toISOString(), to: to.toISOString() };
}

async function load() {
  loading.value = true;
  try {
    const { from, to } = createdRange();
    const data = await client.adminUsers({
      q: q.q,
      status: q.status,
      role: q.role,
      page: q.page,
      size: q.size,
      created_from: from,
      created_to: to,
      sort: sort.value.columnKey,
      // naive 用 ascend/descend，后端用 asc/desc
      order: sort.value.order === "ascend" ? "asc" : "desc",
    });
    items.value = data.items;
    total.value = data.total;
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

function open(id: string) {
  const base = props.basePath ?? "/admin";
  router.push(`${base}/users/${id}`);
}

function create() {
  router.push(`${props.basePath ?? "/admin"}/users/new`);
}

/**
 * 改查询条件后必须回到第一页：否则在第 2 页上搜索只命中少量结果时，
 * 请求会带着旧页码发出去，返回空列表，看起来像「搜不到」。
 */
function search() {
  q.page = 1;
  load();
}

function reset() {
  q.q = "";
  q.status = "";
  q.role = "";
  q.page = 1;
  range.value = null;
  sort.value = { columnKey: "created_at", order: "descend" };
  load();
}

/**
 * 表头排序变化。cancel（order 为 false）时回到默认的注册时间倒序——
 * 列表总得有个确定的顺序，否则翻页拿到的次序不稳定。
 */
function onSorterChange(sorter: DataTableSortState | DataTableSortState[] | null) {
  const s = Array.isArray(sorter) ? sorter[0] : sorter;
  if (!s || !s.order) {
    sort.value = { columnKey: "created_at", order: "descend" };
  } else {
    sort.value = {
      columnKey: s.columnKey as SortKey,
      order: s.order === "ascend" ? "ascend" : "descend",
    };
  }
  q.page = 1;
  load();
}

/**
 * 分页交给服务端，这里每次只拿到一页。
 *
 * n-data-table 必须显式给 remote（见模板）：不设它时 naive 会忽略这里的
 * itemCount，改用「当前这一页数组的长度」当作总数，于是 31 个用户也只算出
 * 1 页、翻不到第 2 页；同时它还会对已经分好页的数组再本地切一次，
 * 第 2 页会渲染成空白。
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

/**
 * 三个可排序字段都用 sorter: true —— 它只提供「可点击 + 箭头」，不提供比较函数。
 * 这是有意为之：远程分页下这里只有一页数据，本地排序会把 10 行排一遍却让
 * 用户以为全量有序。真正次序来自服务端，箭头由受控 sortOrder 反映服务端结果。
 */
const sortOrderFor = (key: SortKey) => (sort.value.columnKey === key ? sort.value.order : false);

const columns = computed<DataTableColumns<User>>(() => [
  { title: "昵称", key: "nickname", sorter: true, sortOrder: sortOrderFor("nickname") },
  { title: "手机", key: "phone", render: (r) => r.phone || "-" },
  { title: "邮箱", key: "email", render: (r) => r.email || "-" },
  {
    title: "角色",
    key: "roles",
    // 后端保证 roles 是数组（RoleCodes 会归一成空切片），这里仍兜一层：
    // 单条记录缺字段不该让整页渲染抛错——渲染函数一抛，router-view 会永久
    // 停在出错前那一页，切路由全部失效，且默认没有任何报错提示。
    render: (r) =>
      h(NSpace, { size: 4 }, () =>
        (r.roles ?? []).map((role) => h(NTag, { size: "small", bordered: false }, { default: () => roleLabel(role) })),
      ),
  },
  {
    title: "状态",
    key: "status",
    render: (r) =>
      h(NTag, { type: r.status === "active" ? "success" : "warning", size: "small" }, { default: () => statusLabel(r.status) }),
  },
  {
    title: "注册时间",
    key: "created_at",
    sorter: true,
    sortOrder: sortOrderFor("created_at"),
    render: (r) => formatDateTime(r.created_at),
  },
  {
    title: "操作",
    key: "actions",
    render: (r) =>
      h(
        NButton,
        { text: true, type: "primary", onClick: () => open(r.id) },
        { icon: () => h("i", { class: "ri-eye-line dauth-ri" }), default: () => "详情" },
      ),
  },
]);

onMounted(load);
</script>

<template>
  <div class="d-auth-admin dauth-page wide">
    <n-card title="用户管理">
      <template #header-extra>
        <n-button type="primary" @click="create">
          <template #icon><i class="ri-user-add-line dauth-ri" /></template>
          创建用户
        </n-button>
      </template>
      <n-space style="margin-bottom: 16px" align="center" wrap>
        <n-input v-model:value="q.q" placeholder="搜索手机 / 邮箱 / 昵称" style="width: 240px" @keyup.enter="search" />
        <n-select v-model:value="q.status" :options="statusOptions" style="width: 140px" />
        <n-date-picker
          v-model:value="range"
          type="daterange"
          clearable
          start-placeholder="注册开始日"
          end-placeholder="注册结束日"
          style="width: 280px"
          @update:value="q.page = 1"
        />
        <n-button type="primary" :loading="loading" @click="search">
          <template #icon><i class="ri-search-line dauth-ri" /></template>
          查询
        </n-button>
        <n-button @click="reset">重置</n-button>
      </n-space>
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        @update:sorter="onSorterChange"
      >
        <template #empty>
          <div class="dauth-empty">
            <i class="ri-inbox-2-line dauth-ri" />
            暂无数据
          </div>
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>
