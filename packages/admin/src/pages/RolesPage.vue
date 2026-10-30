<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NTag,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Role } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { useConfirm } from "../confirm";
import { errMessage } from "../labels";
import { renderActions } from "../table";
import { useTableScrollX } from "../viewport";

const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const tableScrollX = useTableScrollX();
const items = ref<Role[]>([]);
const show = ref(false);
/** 非空表示编辑该角色，为空表示新建。 */
const editingId = ref<string | null>(null);
const form = reactive({ code: "", name: "", description: "" });

async function load() {
  items.value = await client.adminRoles();
}

function openCreate() {
  editingId.value = null;
  form.code = "";
  form.name = "";
  form.description = "";
  show.value = true;
}

function openEdit(role: Role) {
  editingId.value = role.id;
  form.code = role.code;
  form.name = role.name;
  form.description = role.description;
  show.value = true;
}

async function submit() {
  if (!form.name.trim()) {
    message.warning("请填写角色名称");
    return;
  }
  try {
    if (editingId.value) {
      await client.adminUpdateRole(editingId.value, {
        name: form.name.trim(),
        description: form.description.trim(),
      });
      message.success("已保存");
    } else {
      if (!form.code.trim()) {
        message.warning("请填写角色代码");
        return;
      }
      await client.adminCreateRole({
        code: form.code.trim(),
        name: form.name.trim(),
        description: form.description.trim(),
      });
      message.success("已创建");
    }
    show.value = false;
    await load();
  } catch (e: unknown) {
    message.error(errMessage(e, "保存失败"));
  }
}

function remove(role: Role) {
  const affected =
    role.user_count > 0 ? `当前有 ${role.user_count} 个用户使用该角色，删除后这些用户将失去它。` : "";
  confirm({
    title: "确认删除角色",
    content: `确定删除「${role.name}」？${affected}`,
    positiveText: "删除",
    onPositiveClick: async () => {
      try {
        await client.adminDeleteRole(role.id);
        message.success("已删除");
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e, "删除失败"));
      }
    },
  });
}

const columns: DataTableColumns<Role> = [
  { title: "代码", key: "code", width: 130, ellipsis: { tooltip: true } },
  { title: "名称", key: "name", width: 130, ellipsis: { tooltip: true } },
  { title: "说明", key: "description", ellipsis: { tooltip: true } },
  {
    title: "类型",
    key: "is_system",
    width: 100,
    render: (r) =>
      h(
        NTag,
        { size: "small", type: r.is_system ? "warning" : "default" },
        { default: () => (r.is_system ? "系统内置" : "自定义") },
      ),
  },
  { title: "用户数", key: "user_count", width: 80 },
  {
    title: "操作",
    key: "op",
    width: 110,
    // 靠右固定：横向滚动时操作列始终可见
    fixed: "right",
    render: (r) =>
      renderActions([
        { label: "编辑", type: "primary", onClick: () => openEdit(r) },
        // 系统内置角色不允许删除，传 null 而不是渲染禁用按钮，避免误导
        r.is_system ? null : { label: "删除", type: "error", onClick: () => remove(r) },
      ]),
  },
];

onMounted(load);
</script>

<template>
  <n-card title="角色" class="d-auth-admin dauth-page wide">
    <template #header-extra>
      <n-button type="primary" @click="openCreate">
        <template #icon><i class="ri-add-line dauth-ri" /></template>
        新建角色
      </n-button>
    </template>
    <n-data-table :columns="columns" :data="items" :scroll-x="tableScrollX">
      <template #empty>
        <div class="dauth-empty"><i class="ri-inbox-2-line dauth-ri" />暂无数据</div>
      </template>
    </n-data-table>
    <p class="hint">系统内置角色（超级管理员 / 管理员 / 用户）不可删除，可修改显示名称与说明。</p>

    <n-modal
      v-model:show="show"
      preset="card"
      :title="editingId ? '编辑角色' : '新建角色'"
      style="width: 480px"
    >
      <n-form label-placement="top">
        <n-form-item label="代码">
          <!-- 代码是业务系统与 JWT 引用的标识，创建后不可修改 -->
          <n-input
            v-model:value="form.code"
            :disabled="!!editingId"
            placeholder="小写字母开头，如 business_ops"
          />
        </n-form-item>
        <n-form-item label="名称">
          <n-input v-model:value="form.name" placeholder="展示给管理员看的名称" />
        </n-form-item>
        <n-form-item label="说明">
          <n-input v-model:value="form.description" type="textarea" :rows="2" />
        </n-form-item>
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
  color: var(--dauth-text-secondary, #8c8c8c);
}
</style>
