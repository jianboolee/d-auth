<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { NButton, NCard, NForm, NFormItem, NInput, NSelect, useMessage } from "naive-ui";
import { useAdminClient } from "../client";
import { errMessage, roleLabel } from "../labels";

const props = defineProps<{ basePath?: string }>();
const client = useAdminClient();
const router = useRouter();
const message = useMessage();
const form = reactive({ nickname: "", phone: "", email: "", password: "", roles: ["user"] });
const loading = ref(false);
// 角色列表从后端取，自定义角色才能被分配
const roleOptions = ref<{ label: string; value: string }[]>([]);

onMounted(async () => {
  try {
    roleOptions.value = (await client.adminRoles()).map((r) => ({
      label: r.name || roleLabel(r.code),
      value: r.code,
    }));
  } catch (e: unknown) {
    message.error(errMessage(e, "角色加载失败"));
  }
});

async function submit() {
  loading.value = true;
  try {
    await client.adminCreateUser(form);
    message.success("用户已创建");
    router.push(`${props.basePath ?? "/admin"}/users`);
  } catch (e: unknown) {
    message.error(errMessage(e, "创建失败"));
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <n-card title="创建用户" class="d-auth-admin dauth-page" style="max-width: 560px">
    <n-form label-placement="top">
      <n-form-item label="昵称"><n-input v-model:value="form.nickname" /></n-form-item>
      <n-form-item label="手机"><n-input v-model:value="form.phone" /></n-form-item>
      <n-form-item label="邮箱"><n-input v-model:value="form.email" /></n-form-item>
      <n-form-item label="密码"><n-input v-model:value="form.password" type="password" placeholder="至少 8 位" /></n-form-item>
      <n-form-item label="角色">
        <n-select v-model:value="form.roles" multiple :options="roleOptions" />
      </n-form-item>
      <n-button type="primary" :loading="loading" @click="submit">
        <template #icon><i class="ri-user-add-line dauth-ri" /></template>
        创建
      </n-button>
    </n-form>
  </n-card>
</template>
