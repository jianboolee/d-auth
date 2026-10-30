<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import {
  NAlert,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NTag,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ApiKey } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { copyText } from "../clipboard";
import { useConfirm } from "../confirm";
import { formatDateTime } from "../datetime";
import { errMessage, expiryOptions } from "../labels";
import { renderActions } from "../table";
import { sumColumnWidth } from "../table";

const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const items = ref<ApiKey[]>([]);
const loading = ref(false);

/** 创建弹窗：step 为 form 时填表，为 reveal 时展示一次性明文密钥。 */
const show = ref(false);
const step = ref<"form" | "reveal">("form");
const creating = ref(false);
const secret = ref("");
const form = reactive({ name: "", days: 0 });

/** 编辑弹窗（仅改名称）。 */
const showEdit = ref(false);
const saving = ref(false);
const editing = ref<ApiKey | null>(null);
const editName = ref("");

/** 列表里的密钥掩码：sk-前缀…末 4 位。老密钥没有 suffix，退化到只显示前缀。 */
function maskKey(k: ApiKey) {
  return k.key_suffix ? `${k.key_prefix}…${k.key_suffix}` : `${k.key_prefix}…`;
}

async function load() {
  loading.value = true;
  try {
    items.value = await client.adminApiKeys();
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  form.name = "";
  form.days = 0;
  secret.value = "";
  step.value = "form";
  show.value = true;
}

async function create() {
  if (!form.name.trim()) {
    message.warning("请填写名称");
    return;
  }
  creating.value = true;
  try {
    const res = await client.adminCreateApiKey({ name: form.name.trim(), days: form.days });
    secret.value = res.secret;
    step.value = "reveal";
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e, "创建失败"));
  } finally {
    creating.value = false;
  }
}

async function copySecret() {
  if (await copyText(secret.value)) {
    message.success("已复制到剪贴板");
  } else {
    message.error("复制失败，请手动选择复制");
  }
}

function openEdit(k: ApiKey) {
  editing.value = k;
  editName.value = k.name;
  showEdit.value = true;
}

async function saveEdit() {
  if (!editing.value) return;
  if (!editName.value.trim()) {
    message.warning("请填写名称");
    return;
  }
  saving.value = true;
  try {
    await client.adminPatchApiKey(editing.value.id, { name: editName.value.trim() });
    message.success("已保存");
    showEdit.value = false;
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e, "保存失败"));
  } finally {
    saving.value = false;
  }
}

function toggleStatus(k: ApiKey) {
  const next = k.status === "active" ? "disabled" : "active";
  const verb = next === "disabled" ? "停用" : "启用";
  confirm({
    title: `确认${verb}`,
    content:
      next === "disabled"
        ? `停用后「${k.name}」立即失效，正在使用它的服务会拿到 401。可随时重新启用。`
        : `启用后「${k.name}」将恢复可用。`,
    positiveText: verb,
    onPositiveClick: async () => {
      try {
        await client.adminPatchApiKey(k.id, { status: next });
        message.success(`已${verb}`);
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e, `${verb}失败`));
      }
    },
  });
}

function remove(k: ApiKey) {
  // 最近用过说明可能有服务还在用，提示出来，避免误删导致线上中断
  const used = k.last_used_at ? `该密钥最近使用于 ${formatDateTime(k.last_used_at)}，删除后使用它的服务会立即失败。` : "";
  confirm({
    title: "确认删除 API Key",
    content: `确定删除「${k.name}」？删除后不可恢复。${used}`,
    positiveText: "删除",
    onPositiveClick: async () => {
      try {
        await client.adminDeleteApiKey(k.id);
        message.success("已删除");
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e, "删除失败"));
      }
    },
  });
}

/**
 * 操作列一律用文字按钮。
 *
 * 早先这里是「纯图标 + NTooltip(trigger: hover)」，理由是省表格宽度。但触屏没有
 * hover，手机上看到的是三个没有标签的图标，用户只能靠猜；而且图标按钮的点击
 * 区域很小，编辑/停用/删除又挨得很近，容易点错。改成文字后语义自明，配合
 * .dauth-action 的左右内边距也更好点。
 */
const columns: DataTableColumns<ApiKey> = [
  { title: "名称", key: "name", width: 140, ellipsis: { tooltip: true } },
  {
    title: "密钥",
    key: "key_prefix",
    width: 130,
    ellipsis: { tooltip: true },
    render: (r) => h("code", { class: "key-mask" }, maskKey(r)),
  },
  {
    title: "状态",
    key: "status",
    width: 80,
    render: (r) =>
      h(NTag, { size: "small", type: r.status === "active" ? "success" : "warning" }, { default: () => (r.status === "active" ? "正常" : "已停用") }),
  },
  // 三列时间都是定长字符串，给足宽度并开省略，避免把表格撑得过宽
  { title: "创建时间", key: "created_at", width: 165, render: (r) => formatDateTime(r.created_at) },
  {
    title: "到期时间",
    key: "expires_at",
    width: 165,
    render: (r) => (r.expires_at ? formatDateTime(r.expires_at) : "永不过期"),
  },
  { title: "最近使用", key: "last_used_at", width: 165, render: (r) => formatDateTime(r.last_used_at) },
  {
    title: "操作",
    key: "op",
    width: 150,
    // 靠右固定：横向滚动时操作列始终可见
    fixed: "right",
    render: (r) =>
      renderActions([
        { label: "编辑", type: "primary", onClick: () => openEdit(r) },
        {
          label: r.status === "active" ? "停用" : "启用",
          type: "default",
          onClick: () => toggleStatus(r),
        },
        { label: "删除", type: "error", onClick: () => remove(r) },
      ]),
  },
];
const tableScrollX = computed(() => sumColumnWidth(columns));


onMounted(load);
</script>

<template>
  <n-card title="API Keys" class="d-auth-admin dauth-page wide">
    <template #header-extra>
      <n-button type="primary" @click="openCreate">
        <template #icon><i class="ri-add-line dauth-ri" /></template>
        新建
      </n-button>
    </template>
    <n-data-table :columns="columns" :data="items" :loading="loading" :scroll-x="tableScrollX">
      <template #empty>
        <div class="dauth-empty"><i class="ri-inbox-2-line dauth-ri" />暂无数据</div>
      </template>
    </n-data-table>
    <p class="hint">
      API Key 供后端服务调用 d-auth 接口使用，具有完整的用户读写能力，密钥只在创建时显示一次 ——
      请只保存在服务端，不要放进浏览器或客户端，否则会泄露。停用或删除会立即生效。
    </p>

    <!-- 创建：填表 → 展示密钥，两步在同一弹窗内切换 -->
    <n-modal
      v-model:show="show"
      preset="card"
      :title="step === 'form' ? '新建 API Key' : '已创建 API Key'"
      style="width: 520px"
      :mask-closable="false"
      @after-leave="secret = ''"
    >
      <n-form v-if="step === 'form'" label-placement="top">
        <n-form-item label="名称">
          <n-input v-model:value="form.name" placeholder="如 订单服务" />
        </n-form-item>
        <n-form-item label="有效期">
          <n-select v-model:value="form.days" :options="expiryOptions" />
        </n-form-item>
      </n-form>
      <template v-else>
        <n-alert type="warning" :show-icon="true" title="请立即复制，关闭后无法再次查看">
          这是密钥唯一的明文展示，d-auth 只保存它的哈希，之后无法找回。
        </n-alert>
        <div class="dauth-code-row reveal-row">
          <n-input :value="secret" readonly class="key-mask" />
          <n-button type="primary" @click="copySecret">
            <template #icon><i class="ri-file-copy-line dauth-ri" /></template>
            复制
          </n-button>
        </div>
      </template>
      <n-space justify="end" style="margin-top: 12px">
        <template v-if="step === 'form'">
          <n-button @click="show = false">取消</n-button>
          <n-button type="primary" :loading="creating" @click="create">创建</n-button>
        </template>
        <n-button v-else type="primary" @click="show = false">完成</n-button>
      </n-space>
    </n-modal>

    <!-- 编辑：仅改名称 -->
    <n-modal v-model:show="showEdit" preset="card" title="编辑 API Key" style="width: 460px">
      <n-form label-placement="top">
        <n-form-item label="名称"><n-input v-model:value="editName" /></n-form-item>
      </n-form>
      <n-space justify="end" style="margin-top: 12px">
        <n-button @click="showEdit = false">取消</n-button>
        <n-button type="primary" :loading="saving" @click="saveEdit">保存</n-button>
      </n-space>
    </n-modal>
  </n-card>
</template>

<style scoped>
.hint {
  margin: 12px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--dauth-text-secondary, #8c8c8c);
}
/* 明文密钥行：与上方警告留出间距 */
.reveal-row {
  margin-top: 12px;
}
.key-mask {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
}
</style>