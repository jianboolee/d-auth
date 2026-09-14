<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSpace,
  NTag,
  useMessage,
} from "naive-ui";
import type { Identity, Session, User } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";

const props = defineProps<{ basePath?: string }>();
const route = useRoute();
const router = useRouter();
const client = useAdminClient();
const message = useMessage();
const user = ref<User | null>(null);
const identities = ref<Identity[]>([]);
const sessions = ref<Session[]>([]);
const nickname = ref("");
const roles = ref<string[]>([]);
const password = ref("");
const roleOptions = ref<{ label: string; value: string }[]>([]);

async function load() {
  const id = String(route.params.id);
  const [detail, rs] = await Promise.all([client.adminUser(id), client.adminRoles()]);
  user.value = detail.user;
  identities.value = detail.identities;
  sessions.value = detail.sessions;
  nickname.value = detail.user.nickname;
  roles.value = detail.user.roles;
  roleOptions.value = rs.map((r) => ({ label: `${r.name} (${r.code})`, value: r.code }));
}

async function save() {
  if (!user.value) return;
  try {
    user.value = await client.adminPatchUser(user.value.id, { nickname: nickname.value, roles: roles.value });
    message.success("已保存");
  } catch (e: any) {
    message.error(e.message ?? "保存失败");
  }
}

async function setStatus(status: string) {
  if (!user.value) return;
  user.value = await client.adminPatchUser(user.value.id, { status });
  message.success(status === "active" ? "已启用" : "已停用");
}

async function resetPass() {
  if (!user.value || password.value.length < 8) {
    message.warning("密码至少 8 位");
    return;
  }
  await client.adminResetPassword(user.value.id, password.value);
  password.value = "";
  message.success("密码已重置，其它会话已失效");
}

onMounted(load);
</script>

<template>
  <div class="d-auth-admin" v-if="user">
    <n-space vertical size="large">
      <n-button text @click="router.push(`${props.basePath ?? '/admin'}/users`)">返回列表</n-button>
      <n-card title="用户详情">
        <n-descriptions :column="2">
          <n-descriptions-item label="ID">{{ user.id }}</n-descriptions-item>
          <n-descriptions-item label="状态">
            <n-tag :type="user.status === 'active' ? 'success' : 'warning'">{{ user.status }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="手机">{{ user.phone || "-" }}</n-descriptions-item>
          <n-descriptions-item label="邮箱">{{ user.email || "-" }}</n-descriptions-item>
        </n-descriptions>
        <n-form style="margin-top: 20px" label-placement="left" label-width="80">
          <n-form-item label="昵称">
            <n-input v-model:value="nickname" />
          </n-form-item>
          <n-form-item label="角色">
            <n-select v-model:value="roles" multiple :options="roleOptions" />
          </n-form-item>
        </n-form>
        <n-space>
          <n-button type="primary" @click="save">保存</n-button>
          <n-button v-if="user.status === 'active'" @click="setStatus('disabled')">停用</n-button>
          <n-button v-else type="success" @click="setStatus('active')">启用</n-button>
        </n-space>
      </n-card>
      <n-card title="重置密码">
        <n-space>
          <n-input v-model:value="password" type="password" placeholder="新密码" style="width: 240px" />
          <n-button @click="resetPass">重置并踢下线</n-button>
        </n-space>
      </n-card>
      <n-card title="登录标识">
        <div v-for="id in identities" :key="id.id" class="row">{{ id.type }} · {{ id.identifier }}</div>
      </n-card>
      <n-card title="会话">
        <div v-for="s in sessions" :key="s.id" class="row">
          {{ s.ip }} · {{ s.user_agent }} · {{ s.revoked_at ? "已失效" : "有效" }}
        </div>
      </n-card>
    </n-space>
  </div>
</template>

<style scoped>
.row {
  padding: 8px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  font-size: 13px;
  color: #9aa3b2;
}
</style>
