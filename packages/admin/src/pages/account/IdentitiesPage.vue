<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { NButton, NCard, NDataTable, useDialog, useMessage } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import type { Identity } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../../client";
import { useAuthPaths } from "../../paths";
import { errMessage, identityLabel } from "../../labels";

const router = useRouter();
const client = useAdminClient();
const paths = useAuthPaths();
const message = useMessage();
const dialog = useDialog();
const items = ref<Identity[]>([]);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    items.value = await client.identities();
  } catch (e: unknown) {
    message.error(errMessage(e));
  } finally {
    loading.value = false;
  }
}

function goSecurity() {
  router.push(`${paths.accountBase()}/security`);
}

function confirmUnbind(row: Identity) {
  dialog.warning({
    title: "确认解绑",
    content: `解绑后将无法再使用「${identityLabel(row.type)}」登录，且需要至少保留一种登录方式。`,
    positiveText: "解绑",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await client.deleteIdentity(row.id);
        message.success("已解绑");
        await load();
      } catch (e: unknown) {
        message.error(errMessage(e));
      }
    },
  });
}

const columns: DataTableColumns<Identity> = [
  { title: "类型", key: "type", render: (r) => identityLabel(r.type) },
  { title: "标识", key: "identifier" },
  {
    title: "操作",
    key: "op",
    render: (r) => {
      // 手机号 / 邮箱在安全页统一管理，避免与那里重复操作
      if (r.type === "phone" || r.type === "email") {
        return h(
          NButton,
          { text: true, type: "primary", onClick: goSecurity },
          { default: () => "在安全页管理" },
        );
      }
      return h(
        NButton,
        { text: true, type: "error", onClick: () => confirmUnbind(r) },
        {
          icon: () => h("i", { class: "ri-link-unlink-m dauth-ri" }),
          default: () => "解绑",
        },
      );
    },
  },
];

onMounted(load);
</script>

<template>
  <n-card title="登录方式" class="dauth-page wide">
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
