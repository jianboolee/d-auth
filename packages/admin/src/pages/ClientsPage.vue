<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import { NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NTag, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { ClientApp } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { useConfirm } from "../confirm";
import { clientTypeLabel, clientTypeOptions, errMessage, statusLabel } from "../labels";
import { renderActions } from "../table";
import { useTableScrollX } from "../viewport";

const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const tableScrollX = useTableScrollX();
const items = ref<ClientApp[]>([]);
const loading = ref(false);
const show = ref(false);
/** 非空表示编辑该应用，为空表示新建。 */
const editingId = ref<string | null>(null);
/** 编辑中的这一行是否控制台自身（其标识不允许改，见下）。 */
const editingConsole = ref(false);
const form = reactive({
  client_key: "",
  name: "",
  type: "miniprogram",
  wechat_app_id: "",
  wechat_app_secret: "",
});

/**
 * 控制台自身的应用标识，对应后端 models.ClientKeyConsole。
 *
 * web/src/api/client.ts 硬编码了它，登录流程在缺少 X-DAuth-Client 时也回退到它；
 * 改名或删除会让控制台发出的请求对不上应用，且要改代码重新部署才能恢复。后端已
 * 拒绝这两件事，这里同步把按钮收起来，免得管理员点下去才拿到报错。
 */
const CONSOLE_CLIENT_KEY = "web_console";

const isConsole = (r: ClientApp) => r.client_key === CONSOLE_CLIENT_KEY;

/**
 * AppID / AppSecret 只有小程序登录会用到。
 *
 * 后端只有 WeChatMini 一个函数读这两个字段，它们最终作为 appid / secret 传给
 * 微信的 sns/jscode2session。网页扫码登录用的是服务端级环境变量
 * （DAUTH_WECHAT_WEB_APP_ID / _SECRET），不按应用配置。所以 web 与 App 类型
 * 下这两个字段没有意义，展示出来只会让人不知道该填什么。
 */
const isMiniProgram = computed(() => form.type === "miniprogram");

async function load() {
  loading.value = true;
  try {
    items.value = await client.adminClients();
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  form.client_key = "";
  form.name = "";
  form.type = "miniprogram";
  form.wechat_app_id = "";
  form.wechat_app_secret = "";
}

function openCreate() {
  editingId.value = null;
  editingConsole.value = false;
  // 每次打开都从干净的表单开始，否则上一次的输入会残留
  resetForm();
  show.value = true;
}

function openEdit(row: ClientApp) {
  editingId.value = row.id;
  editingConsole.value = isConsole(row);
  form.client_key = row.client_key;
  form.name = row.name;
  form.type = row.type;
  form.wechat_app_id = row.wechat_app_id;
  // 密钥只存密文、不回显，留空即表示不修改
  form.wechat_app_secret = "";
  show.value = true;
}

async function submit() {
  if (!form.name.trim()) {
    message.warning("请填写应用名称");
    return;
  }
  if (!editingId.value && !form.client_key.trim()) {
    message.warning("请填写应用标识");
    return;
  }
  try {
    const body = {
      name: form.name.trim(),
      type: form.type,
      wechat_app_id: form.wechat_app_id.trim(),
      ...(form.wechat_app_secret ? { wechat_app_secret: form.wechat_app_secret } : {}),
    };
    if (editingId.value) {
      await client.adminPatchClient(editingId.value, {
        ...body,
        // 控制台的标识不可改：不发这个字段（后端也会拒绝）
        ...(editingConsole.value ? {} : { client_key: form.client_key.trim() }),
      });
      message.success("已保存");
    } else {
      await client.adminCreateClient({ ...body, client_key: form.client_key.trim() });
      message.success("已创建");
    }
    show.value = false;
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e, "保存失败"));
  }
}

function toggleStatus(row: ClientApp) {
  const next = row.status === "active" ? "disabled" : "active";
  const verb = next === "disabled" ? "停用" : "启用";
  confirm({
    title: `确认${verb}`,
    content:
      next === "disabled"
        ? `停用后「${row.name}」将无法再用于登录：小程序换不到会话，新的登录也不再归属该应用。可随时重新启用。`
        : `启用后「${row.name}」将重新可用于登录。`,
    positiveText: verb,
    onPositiveClick: async () => {
      try {
        await client.adminPatchClient(row.id, { status: next });
        message.success(`已${verb}`);
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e, `${verb}失败`));
      }
    },
  });
}

function remove(row: ClientApp) {
  confirm({
    title: "确认删除应用",
    content: `确定删除「${row.name}」（${row.client_key}）？删除后不可恢复，已接入的项目会立即失效。该应用名下的登录会话会保留，只是不再归属任何应用。`,
    positiveText: "删除",
    onPositiveClick: async () => {
      try {
        await client.adminDeleteClient(row.id);
        message.success("已删除");
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e, "删除失败"));
      }
    },
  });
}

const columns: DataTableColumns<ClientApp> = [
  { title: "应用标识", key: "client_key", width: 130, ellipsis: { tooltip: true } },
  { title: "名称", key: "name", width: 130, ellipsis: { tooltip: true } },
  { title: "类型", key: "type", width: 90, render: (r) => clientTypeLabel(r.type) },
  {
    title: "小程序 AppID",
    key: "wechat_app_id",
    width: 190,
    ellipsis: { tooltip: true },
    // 只有小程序才有值，其余类型留空会是一片空白格
    render: (r) => r.wechat_app_id || "—",
  },
  {
    title: "状态",
    key: "status",
    width: 80,
    render: (r) => h(NTag, { size: "small", type: r.status === "active" ? "success" : "warning" }, { default: () => statusLabel(r.status) }),
  },
  {
    title: "操作",
    key: "op",
    width: 130,
    // 靠右固定：横向滚动时操作列始终可见
    fixed: "right",
    render: (r) =>
      renderActions([
        { label: "编辑", type: "primary", onClick: () => openEdit(r) },
        // 控制台自身既不能停用也不能删除，传 null 而非渲染禁用按钮
        isConsole(r) ? null : { label: r.status === "active" ? "停用" : "启用", type: "default", onClick: () => toggleStatus(r) },
        isConsole(r) ? null : { label: "删除", type: "error", onClick: () => remove(r) },
      ]),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="应用 / 小程序" class="d-auth-admin dauth-page wide">
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
      停用会立即生效（该应用无法再用于登录），删除则不可恢复；控制台自身的应用（{{ CONSOLE_CLIENT_KEY }}）不支持这两项。
      跨域来源白名单由服务端环境变量 DAUTH_ALLOWED_ORIGINS 控制。
    </p>

    <n-modal v-model:show="show" preset="card" :title="editingId ? '编辑应用' : '新建应用'" style="width: 520px">
      <n-form label-placement="top">
        <n-form-item label="应用标识">
          <!-- 控制台自身依赖这个标识（前端硬编码），改写后要改代码重新部署，故禁用 -->
          <n-input
            v-model:value="form.client_key"
            :disabled="editingConsole"
            placeholder="小写字母开头，如 web_ticket"
          />
        </n-form-item>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="类型">
          <n-select v-model:value="form.type" :options="clientTypeOptions" />
        </n-form-item>
        <!-- 只有小程序登录会读这两个字段，其它类型填了也不会被用到 -->
        <template v-if="isMiniProgram">
          <n-form-item label="AppID（小程序 ID）">
            <n-input v-model:value="form.wechat_app_id" placeholder="wx 开头，如 wx1234567890abcdef" />
          </n-form-item>
          <n-form-item :label="editingId ? 'AppSecret（小程序密钥，留空表示不修改）' : 'AppSecret（小程序密钥）'">
            <n-input v-model:value="form.wechat_app_secret" type="password" />
          </n-form-item>
          <p class="hint field-hint">
            两者来自微信公众平台「开发管理 → 开发设置」，仅小程序登录需要；网页扫码登录用的是服务端环境变量。
          </p>
        </template>
      </n-form>
      <n-space justify="end" style="margin-top: 12px">
        <n-button @click="show = false">取消</n-button>
        <n-button type="primary" @click="submit">保存</n-button>
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
/* 表单内的说明紧跟字段，去掉与上方留白重复的顶边距 */
.field-hint {
  margin: -8px 0 12px;
}
</style>
