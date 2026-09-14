<script setup lang="ts">
import { computed } from "vue";
import { zhCN, dateZhCN, NConfigProvider, NMessageProvider, NGlobalStyle } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";
import { storeToRefs } from "pinia";
import { provideAuthSession } from "@donktech/d-auth-admin";
import { useAuthStore } from "./stores/auth";

const auth = useAuthStore();
const { user } = storeToRefs(auth);
provideAuthSession({
  user,
  setUser: (u) => {
    auth.user = u;
  },
});

const themeOverrides = computed<GlobalThemeOverrides>(() => ({
  common: {
    primaryColor: "#1a1a1a",
    primaryColorHover: "#333333",
    primaryColorPressed: "#000000",
    primaryColorSuppl: "#1a1a1a",
    borderRadius: "8px",
    fontFamily: '"PingFang SC", "Hiragino Sans GB", "Noto Sans SC", sans-serif',
  },
}));
</script>

<template>
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN" :theme-overrides="themeOverrides">
    <n-global-style />
    <n-message-provider>
      <router-view />
    </n-message-provider>
  </n-config-provider>
</template>
