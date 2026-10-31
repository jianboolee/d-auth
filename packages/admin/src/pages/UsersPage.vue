<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import {
  NCard,
  NDataTable,
  NDatePicker,
  NEllipsis,
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
import { useCompactColumns } from "../viewport";
import { ACTION_COLUMN_WIDTH, renderActions, sumColumnWidth } from "../table";

const props = defineProps<{ basePath?: string }>();
const client = useAdminClient();
const router = useRouter();
const message = useMessage();
const compact = useCompactColumns();
/**
 * 窄屏下筛选区默认收起，只留一个模糊搜索框：375px 屏上搜索/状态/日期三个
 * 控件加两个按钮会堆成四五行的竖向表单，把列表挤到首屏之外。
 * 展开状态只在本次会话内有效，切页不持久化——避免「上次展开过，这次以为
 * 筛选没生效」的困惑。
 */
const filtersOpen = ref(false);
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

/**
 * 昵称单元格：头像常驻在昵称左边，与昵称同属一列。
 *
 * 不单开头像列：一格只有 24px 内容却要占掉一整列宽，窄屏上尤其亏；头像和昵称
 * 本来就是一体的识别信息，放同一格更紧凑。
 *
 * 没有头像时不画首字母、也不放图标，只留一个灰底圆做占位——占位本身已经表达
 * 「这里该有头像」，填字反而在列表里堆出一片杂讯。
 *
 * 样式全部内联：render 里的节点由 naive 的 Cell 组件在它自己的渲染期创建，
 * 拿不到本组件的 scoped 样式，写进 <style scoped> 不会生效。
 */
function renderNickname(r: User) {
  return h(
    "div",
    { style: { display: "flex", alignItems: "center", gap: "8px", minWidth: 0 } },
    [
      h(
        "span",
        {
          style: {
            flex: "0 0 auto",
            width: "24px",
            height: "24px",
            borderRadius: "50%",
            overflow: "hidden",
            background: "var(--dauth-avatar-placeholder, #ececef)",
          },
        },
        r.avatar_url
          ? [
              h("img", {
                src: r.avatar_url,
                alt: "",
                loading: "lazy",
                // 加载失败就隐藏图片，露出下面的灰底圆——否则浏览器会画一个
                // 破图图标，比没有头像更难看。
                onError: (e: Event) => {
                  (e.target as HTMLImageElement).style.visibility = "hidden";
                },
                style: { display: "block", width: "100%", height: "100%", objectFit: "cover" },
              }),
            ]
          : [],
      ),
      // 列本身不开 ellipsis：那样截断的是整个 flex 行，溢出时被裁掉的是头像
      // 而不是昵称文字。这里只让昵称自己省略，并带上 tooltip 看全名。
      // flex 用 0 1 auto 而不是 1 1 auto：昵称不该被拉宽，宽度就取内容宽，
      // NEllipsis 靠「内层是否比外层宽」判断要不要出 tooltip，拉宽会让它误报。
      h(
        NEllipsis,
        { tooltip: true, style: { flex: "0 1 auto", minWidth: 0 } },
        { default: () => r.nickname || "-" },
      ),
    ],
  );
}

/**
 * 列定义随屏宽变化。
 *
 * 窄屏保留四列：昵称、手机、状态、操作。手机号是这一页最主要的识别信息，
 * 必须能看到；邮箱往往比手机号更长、在窄屏上挤成省略号后几乎没有信息量，
 * 角色/注册时间同理，收进详情页查看。
 *
 * 每列都给 width 并开 ellipsis：不给宽度时 naive 按内容自然宽度分配，长邮箱
 * 会把整表撑到远超 640px 的 scroll-x 下限，用户得横向拖很久才能碰到操作列。
 */
const columns = computed<DataTableColumns<User>>(() => {
  const isMobile = compact.value;

  const base: DataTableColumns<User> = [
    {
      title: "昵称",
      key: "nickname",
      // 头像占 24px + 8px 间距，再加上单元格左右内边距，120px 只够 4 个汉字，
      // 「调试管理员」这类 5 字名会被截断。给到 130px 能放下 5 字。
      width: isMobile ? 128 : 130,
      sorter: true,
      sortOrder: sortOrderFor("nickname"),
      render: renderNickname,
    },
    // 手机号通常 11 位，给 120px 能完整显示；脱敏后更短
    { title: "手机", key: "phone", width: 120, ellipsis: { tooltip: true }, render: (r) => r.phone || "-" },
  ];

  if (!isMobile) {
    base.push({
      title: "邮箱",
      key: "email",
      // 从 200 收到 150：昵称列加上头像后需要更多宽度，各列 width 之和必须留在
      // 容器宽度以内，否则整表出现横向滚动、右起固定列会把「注册时间」盖住。
      // 邮箱本来就常被省略成省略号，让出这段宽度损失的信息最少。
      width: 150,
      ellipsis: { tooltip: true },
      render: (r) => r.email || "-",
    });
  }

  if (!isMobile) {
    base.push({
      title: "角色",
      key: "roles",
      width: 140,
      // 这一列是多标签，溢出时按行内截断，tooltip 列出全部
      ellipsis: { tooltip: true },
      // 后端保证 roles 是数组（RoleCodes 会归一成空切片），这里仍兜一层：
      // 单条记录缺字段不该让整页渲染抛错——渲染函数一抛，router-view 会永久
      // 停在出错前那一页，切路由全部失效，且默认没有任何报错提示。
      render: (r) =>
        h(NSpace, { size: 4 }, () =>
          (r.roles ?? []).map((role) => h(NTag, { size: "small", bordered: false }, { default: () => roleLabel(role) })),
        ),
    });
  }

  base.push({
    title: "状态",
    key: "status",
    width: 80,
    render: (r) =>
      h(NTag, { type: r.status === "active" ? "success" : "warning", size: "small" }, { default: () => statusLabel(r.status) }),
  });

  if (!isMobile) {
    base.push({
      title: "注册时间",
      key: "created_at",
      width: 170,
      sorter: true,
      sortOrder: sortOrderFor("created_at"),
      render: (r) => formatDateTime(r.created_at),
    });
  }

  base.push({
    title: "操作",
    key: "actions",
    width: isMobile ? 80 : ACTION_COLUMN_WIDTH,
    // 靠右固定：表格横向滚动时操作列始终可见，不必拖到底才能点
    fixed: "right",
    render: (r) =>
      renderActions([{ label: "详情", type: "primary", onClick: () => open(r.id) }]),
  });

  return base;
});

/**
 * 窄屏下让表格宽度贴合精简后的列总宽。
 *
 * 固定用 640px（MOBILE_TABLE_MIN_WIDTH）是为七列全显示时代设的经验值；现在
 * 窄屏只剩四列、实宽约 390px，仍按 640 撑会凭空多出一段空白滚动区。
 * 宽屏保留 undefined，让 naive 自适应填满容器。
 */
const scrollX = computed(() => (compact.value ? sumColumnWidth(columns.value) : undefined));

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
      <!--
        筛选区。宽屏一行排开；窄屏默认收起，只保留模糊搜索框 + 一个筛选按钮，
        点开后展开状态/日期与查询/重置。搜索框回车即查询，与展开与否无关。
      -->
      <div class="filters">
        <div class="filters-row">
          <n-input
            v-model:value="q.q"
            placeholder="搜索手机 / 邮箱 / 昵称"
            class="filter-q"
            clearable
            @keyup.enter="search"
          />
          <n-button v-if="compact" class="filter-toggle" @click="filtersOpen = !filtersOpen">
            <template #icon>
              <i :class="filtersOpen ? 'ri-filter-fill' : 'ri-filter-line'" class="dauth-ri" />
            </template>
            筛选
          </n-button>
          <template v-else>
            <n-select v-model:value="q.status" :options="statusOptions" class="filter-status" />
            <n-date-picker
              v-model:value="range"
              type="daterange"
              clearable
              start-placeholder="注册开始日"
              end-placeholder="注册结束日"
              class="filter-date"
              @update:value="q.page = 1"
            />
            <n-button type="primary" :loading="loading" @click="search">
              <template #icon><i class="ri-search-line dauth-ri" /></template>
              查询
            </n-button>
            <n-button @click="reset">重置</n-button>
          </template>
        </div>

        <!-- 窄屏展开后的高级筛选 -->
        <div v-if="compact && filtersOpen" class="filters-advanced">
          <n-select v-model:value="q.status" :options="statusOptions" style="width: 100%" />
          <n-date-picker
            v-model:value="range"
            type="daterange"
            clearable
            start-placeholder="注册开始日"
            end-placeholder="注册结束日"
            style="width: 100%"
            @update:value="q.page = 1"
          />
          <div class="filters-actions">
            <n-button type="primary" :loading="loading" style="flex: 1" @click="search">查询</n-button>
            <n-button style="flex: 1" @click="reset">重置</n-button>
          </div>
        </div>
      </div>
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :scroll-x="scrollX"
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

<style scoped>
/* 筛选区：宽屏一行排开；窄屏用 .filter-q 撑满 + 筛选按钮定宽 */
.filters {
  margin-bottom: 16px;
}
.filters-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.filter-q {
  width: 240px;
  /* 常驻的搜索框在窄屏独占一行，避免和筛选按钮挤成两行 */
  flex: 1 1 240px;
  min-width: 0;
}
.filter-status {
  width: 140px;
}
.filter-date {
  width: 280px;
}
.filter-toggle {
  flex: 0 0 auto;
}
/* 窄屏高级筛选：竖向排列、控件撑满，避免固定像素宽度溢出 */
.filters-advanced {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}
.filters-actions {
  display: flex;
  gap: 8px;
}

@media (max-width: 767px) {
  .filter-q {
    /* 与筛选按钮同排，按钮约 76px + 8px 间距 */
    flex: 1 1 auto;
    width: auto;
  }
  .filters-row {
    flex-wrap: nowrap;
  }
}
</style>
