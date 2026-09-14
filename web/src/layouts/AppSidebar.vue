<script setup lang="ts">
import { computed, h } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NAvatar, NButton, NDropdown, NMenu, NTooltip } from "naive-ui";
import type { DropdownOption, MenuOption } from "naive-ui";
import { useAuthPaths } from "@donktech/d-auth-admin";
import { useAuthStore } from "../stores/auth";

const props = defineProps<{
  collapsed?: boolean;
  /** 移动端抽屉里隐藏收起开关 */
  dense?: boolean;
}>();

const emit = defineEmits<{
  toggle: [];
  navigate: [];
}>();

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const paths = useAuthPaths();

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

const sectionTitle = computed(() => (inAdmin.value ? "管理后台" : "账户中心"));
const initial = computed(() => auth.user?.nickname?.slice(0, 1) || "?");

const userOptions = computed<DropdownOption[]>(() => {
  const items: DropdownOption[] = [
    { label: "个人资料", key: "profile", icon: ri("ri-user-settings-line") },
    { label: "账号安全", key: "security", icon: ri("ri-shield-keyhole-line") },
  ];
  if (inAdmin.value) {
    items.push({ label: "返回账户中心", key: "toAccount", icon: ri("ri-user-line") });
  } else if (auth.isAdmin) {
    items.push({ label: "管理后台", key: "toAdmin", icon: ri("ri-settings-3-line") });
  }
  items.push({ label: "退出登录", key: "logout", icon: ri("ri-logout-box-r-line") });
  return items;
});

function go(key: string) {
  emit("navigate");
  router.push(key);
}

async function onUserSelect(key: string) {
  emit("navigate");
  switch (key) {
    case "profile":
      router.push(paths.profile());
      return;
    case "security":
      router.push(`${paths.accountBase()}/security`);
      return;
    case "toAccount":
      router.push(paths.profile());
      return;
    case "toAdmin":
      router.push(paths.adminUsers());
      return;
    case "logout":
      await auth.logout();
      router.push(paths.login());
      return;
  }
}
</script>

<template>
  <div class="sidebar" :class="{ collapsed: props.collapsed }">
    <div class="head">
      <img v-if="!props.collapsed" class="logo" src="/logo.png" alt="logo" />
      <span v-if="!props.collapsed" class="section">{{ sectionTitle }}</span>
      <n-tooltip v-if="!props.dense" placement="right" :delay="300">
        <template #trigger>
          <n-button
            class="toggle"
            quaternary
            circle
            size="small"
            :aria-label="props.collapsed ? '展开侧栏' : '收起侧栏'"
            @click="emit('toggle')"
          >
            <template #icon><i class="ri-layout-left-line dauth-ri" /></template>
          </n-button>
        </template>
        {{ props.collapsed ? "展开侧栏" : "收起侧栏" }}
      </n-tooltip>
    </div>

    <div class="nav">
      <n-menu
        :value="active"
        :options="options"
        :collapsed="props.collapsed"
        :collapsed-width="56"
        :collapsed-icon-size="20"
        :root-indent="18"
        @update:value="go"
      />
    </div>

    <div class="foot">
      <n-dropdown
        trigger="click"
        placement="top-start"
        :options="userOptions"
        :style="{ minWidth: '200px' }"
        @select="onUserSelect"
      >
        <button class="user" type="button" :title="auth.user?.nickname || ''">
          <n-avatar :src="auth.user?.avatar_url" :size="28" round>
            <template #fallback>{{ initial }}</template>
          </n-avatar>
          <template v-if="!props.collapsed">
            <span class="name">{{ auth.user?.nickname || "未登录" }}</span>
          </template>
        </button>
      </n-dropdown>
    </div>
  </div>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--dauth-sidebar-bg);
}
.collapsed {
  align-items: stretch;
}

.head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 10px 10px;
}
.logo {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  object-fit: contain;
  flex-shrink: 0;
  margin-left: 6px;
}
.section {
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  color: var(--dauth-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.toggle {
  flex-shrink: 0;
  color: var(--dauth-text-secondary);
}
.toggle:hover {
  color: var(--dauth-text);
}
.collapsed .head {
  justify-content: center;
  padding-left: 0;
  padding-right: 0;
}
.collapsed .logo {
  margin-left: 0;
}

.nav {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px 8px;
}
.nav :deep(.n-menu-item-content) {
  border-radius: 8px;
}
/* 收起态：菜单宽度需与 n-menu 的 collapsed-width 一致，
   naive-ui 才按 (collapsedWidth - 图标宽)/2 正确居中图标。
   侧栏 64px - 两侧 4px = 56px，故模板里 collapsed-width 设为 56。 */
.collapsed .nav {
  padding: 4px;
}

.foot {
  padding: 8px;
  margin-top: auto;
}
.user {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px;
  border: none;
  border-radius: 8px;
  background: transparent;
  cursor: pointer;
  font: inherit;
  color: var(--dauth-text);
  text-align: left;
}
.user:hover {
  background: var(--dauth-sidebar-hover);
}
.name {
  flex: 1;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.collapsed .foot {
  display: flex;
  justify-content: center;
}
.collapsed .user {
  width: auto;
  padding: 6px;
}
</style>
