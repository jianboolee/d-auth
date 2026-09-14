<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NLayout,
  NLayoutSider,
  NLayoutContent,
  NMenu,
  NButton,
  NSpace,
  NDrawer,
  NDrawerContent,
} from "naive-ui";
import type { MenuOption } from "naive-ui";
import { useAuthStore } from "../stores/auth";
import { useAuthPaths } from "@donktech/d-auth-admin";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const paths = useAuthPaths();
const drawer = ref(false);
const mobile = ref(false);

const embed = computed(
  () =>
    route.meta.embed === true ||
    route.path.startsWith("/embed") ||
    route.query.embed === "1" ||
    (typeof window !== "undefined" && window.self !== window.top),
);

const publicPage = computed(() => route.meta.public === true);

function ri(cls: string) {
  return () => h("i", { class: `${cls} dauth-ri` });
}

const accountMenu = computed<MenuOption[]>(() => {
  const base = paths.accountBase();
  return [
    { label: "资料", key: `${base}/profile`, icon: ri("ri-user-line") },
    { label: "安全", key: `${base}/security`, icon: ri("ri-shield-keyhole-line") },
    { label: "登录方式", key: `${base}/identities`, icon: ri("ri-link") },
    { label: "会话", key: `${base}/sessions`, icon: ri("ri-device-line") },
  ];
});

const adminMenu = computed<MenuOption[]>(() => {
  const base = paths.adminBase();
  return [
    { label: "用户", key: `${base}/users`, icon: ri("ri-team-line") },
    { label: "角色", key: `${base}/roles`, icon: ri("ri-user-star-line") },
    { label: "应用 / 小程序", key: `${base}/clients`, icon: ri("ri-apps-2-line") },
    { label: "接口密钥", key: `${base}/api-keys`, icon: ri("ri-key-2-line") },
    { label: "审计", key: `${base}/audit`, icon: ri("ri-file-list-3-line") },
  ];
});

const inAdmin = computed(() => route.path.includes("/admin"));
const options = computed(() => (inAdmin.value ? adminMenu.value : accountMenu.value));
const active = computed(() => route.path);
const title = computed(() => (typeof route.meta.title === "string" ? route.meta.title : "账户中心"));

function onResize() {
  mobile.value = window.innerWidth < 768;
}

onMounted(() => {
  onResize();
  window.addEventListener("resize", onResize);
});
onUnmounted(() => window.removeEventListener("resize", onResize));

async function logout() {
  await auth.logout();
  router.push(paths.login());
}

function go(key: string) {
  drawer.value = false;
  router.push(key);
}
</script>

<template>
  <n-layout :class="['shell', embed && 'embed']" :has-sider="!mobile" v-if="!embed && !publicPage">
    <n-layout-sider
      v-if="!mobile"
      bordered
      :width="220"
      :native-scrollbar="false"
      content-style="padding: 12px 8px; background: var(--dauth-sidebar-bg)"
    >
      <div class="brand">账户中心</div>
      <n-menu :value="active" :options="options" :collapsed-width="64" @update:value="go" />
    </n-layout-sider>
    <n-layout>
      <div class="top">
        <n-space align="center">
          <n-button v-if="mobile" quaternary circle @click="drawer = true">
            <template #icon><i class="ri-menu-line dauth-ri" /></template>
          </n-button>
          <n-button quaternary @click="router.push(paths.profile())">
            <template #icon><i class="ri-user-line dauth-ri" /></template>
            账号
          </n-button>
          <n-button v-if="auth.isAdmin" quaternary @click="router.push(paths.adminUsers())">
            <template #icon><i class="ri-settings-3-line dauth-ri" /></template>
            管理后台
          </n-button>
        </n-space>
        <n-space align="center">
          <span class="who">{{ auth.user?.nickname }}</span>
          <n-button size="small" @click="logout">
            <template #icon><i class="ri-logout-box-r-line dauth-ri" /></template>
            退出
          </n-button>
        </n-space>
      </div>
      <n-layout-content content-style="padding: 24px" :native-scrollbar="false">
        <router-view />
      </n-layout-content>
    </n-layout>
    <n-drawer v-model:show="drawer" placement="left" :width="240">
      <n-drawer-content title="账户中心" closable>
        <n-menu :value="active" :options="options" @update:value="go" />
      </n-drawer-content>
    </n-drawer>
  </n-layout>

  <n-layout v-else class="shell embed">
    <div v-if="embed && !publicPage" class="top embed-top">
      <span class="embed-title">{{ title }}</span>
      <n-space align="center">
        <n-button v-if="auth.isAdmin" quaternary size="small" @click="router.push(paths.adminUsers())">管理</n-button>
        <n-button v-else quaternary size="small" @click="router.push(paths.profile())">账号</n-button>
        <n-button size="small" @click="logout">
          <template #icon><i class="ri-logout-box-r-line dauth-ri" /></template>
          退出
        </n-button>
      </n-space>
    </div>
    <n-layout-content :content-style="publicPage ? 'padding: 16px' : 'padding: 16px'" :native-scrollbar="false">
      <router-view />
    </n-layout-content>
  </n-layout>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  background: var(--dauth-bg);
}
.brand {
  font-weight: 650;
  padding: 8px 16px 16px;
  color: var(--dauth-text);
}
.top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 20px;
  border-bottom: 1px solid var(--dauth-border);
  background: var(--dauth-surface);
}
.embed-top {
  padding: 8px 16px;
}
.embed-title {
  font-weight: 600;
  font-size: 15px;
}
.who {
  font-size: 13px;
  color: var(--dauth-text-secondary);
}
</style>
