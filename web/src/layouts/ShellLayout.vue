<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NButton, NDrawer, NDrawerContent, NLayout, NLayoutContent, NLayoutSider, NSpace } from "naive-ui";
import { useAuthStore } from "../stores/auth";
import { useAuthPaths } from "@donktech/d-auth-admin";
import AppSidebar from "./AppSidebar.vue";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const paths = useAuthPaths();
const drawer = ref(false);
const collapsed = ref(false);
const mobile = ref(false);

const embed = computed(
  () =>
    route.meta.embed === true ||
    route.path.startsWith("/embed") ||
    route.query.embed === "1" ||
    (typeof window !== "undefined" && window.self !== window.top),
);

const publicPage = computed(() => route.meta.public === true);
const title = computed(() => (typeof route.meta.title === "string" ? route.meta.title : "账户中心"));
// 移动端顶部留出悬浮菜单按钮的位置
const contentPadding = computed(() => (mobile.value ? "56px 16px 20px" : "20px 24px"));

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
</script>

<template>
  <n-layout class="shell" :has-sider="!mobile" v-if="!embed && !publicPage">
    <n-layout-sider
      v-if="!mobile"
      :collapsed="collapsed"
      collapse-mode="width"
      :width="240"
      :collapsed-width="64"
      :content-style="`background: var(--dauth-sidebar-bg)`"
    >
      <app-sidebar :collapsed="collapsed" @toggle="collapsed = !collapsed" />
    </n-layout-sider>

    <n-layout-content class="content" :native-scrollbar="false" :content-style="`padding: ${contentPadding}`">
      <router-view />
    </n-layout-content>

    <n-button
      v-if="mobile"
      class="float-menu"
      circle
      secondary
      title="打开导航"
      @click="drawer = true"
    >
      <template #icon><i class="ri-menu-line dauth-ri" /></template>
    </n-button>

    <n-drawer v-model:show="drawer" placement="left" :width="240">
      <n-drawer-content :native-scrollbar="false" body-content-style="padding: 0">
        <app-sidebar dense @navigate="drawer = false" />
      </n-drawer-content>
    </n-drawer>
  </n-layout>

  <n-layout v-else class="shell embed">
    <div v-if="embed && !publicPage" class="embed-top">
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
    <n-layout-content content-style="padding: 16px" :native-scrollbar="false">
      <router-view />
    </n-layout-content>
  </n-layout>
</template>

<style scoped>
.shell {
  height: 100vh;
}
.shell.embed {
  height: auto;
  min-height: 100vh;
}
.content {
  height: 100%;
  overflow: auto;
  background: var(--dauth-bg);
}
.embed-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 16px;
}
.embed-title {
  font-weight: 600;
  font-size: 15px;
}
.float-menu {
  position: fixed;
  top: 14px;
  left: 14px;
  z-index: 20;
}
</style>
