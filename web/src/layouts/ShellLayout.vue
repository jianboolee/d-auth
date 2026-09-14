<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NLayout, NLayoutSider, NLayoutContent, NMenu, NButton, NSpace } from "naive-ui";
import type { MenuOption } from "naive-ui";
import { useAuthStore } from "../stores/auth";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();

const accountMenu: MenuOption[] = [
  { label: "资料", key: "/account/profile" },
  { label: "安全", key: "/account/security" },
  { label: "登录方式", key: "/account/identities" },
  { label: "会话", key: "/account/sessions" },
];

const adminMenu: MenuOption[] = [
  { label: "用户", key: "/admin/users" },
  { label: "创建用户", key: "/admin/users/new" },
  { label: "角色", key: "/admin/roles" },
  { label: "应用 / 小程序", key: "/admin/clients" },
  { label: "API Key", key: "/admin/api-keys" },
  { label: "审计", key: "/admin/audit" },
];

const inAdmin = computed(() => route.path.startsWith("/admin"));
const options = computed(() => (inAdmin.value ? adminMenu : accountMenu));
const active = computed(() => route.path);

async function logout() {
  await auth.logout();
  router.push("/login");
}
</script>

<template>
  <n-layout has-sider style="min-height: 100vh">
    <n-layout-sider bordered :width="220" content-style="padding: 16px 8px">
      <div class="brand">d-auth</div>
      <n-menu :value="active" :options="options" @update:value="(k: string) => router.push(k)" />
    </n-layout-sider>
    <n-layout>
      <div class="top">
        <n-space>
          <n-button quaternary @click="router.push('/account/profile')">账号</n-button>
          <n-button v-if="auth.isAdmin" quaternary @click="router.push('/admin/users')">管理后台</n-button>
        </n-space>
        <n-space align="center">
          <span class="who">{{ auth.user?.nickname }}</span>
          <n-button size="small" @click="logout">退出</n-button>
        </n-space>
      </div>
      <n-layout-content content-style="padding: 24px">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.brand {
  font-weight: 700;
  letter-spacing: 0.08em;
  padding: 8px 16px 16px;
}
.top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 24px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.who {
  font-size: 13px;
  color: #9aa3b2;
}
</style>
