<script setup lang="ts">
import { computed } from "vue";
import { zhCN, dateZhCN, NConfigProvider, NDialogProvider, NMessageProvider, NGlobalStyle } from "naive-ui";
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
    primaryColor: "#18181b",
    primaryColorHover: "#333336",
    primaryColorPressed: "#000000",
    primaryColorSuppl: "#18181b",
    borderRadius: "8px",
    fontFamily: '"PingFang SC", "Hiragino Sans GB", "Noto Sans SC", sans-serif',
  },
  // 侧栏菜单：用背景深浅表达选中/悬停，避免 naive 默认的主色块与描边
  Menu: {
    itemHeight: "38px",
    borderRadius: "8px",
    itemColorHover: "rgba(0, 0, 0, 0.045)",
    itemColorActive: "rgba(0, 0, 0, 0.07)",
    itemColorActiveHover: "rgba(0, 0, 0, 0.07)",
    itemColorActiveCollapsed: "rgba(0, 0, 0, 0.07)",
    itemTextColor: "#3f3f46",
    itemTextColorHover: "#18181b",
    itemTextColorActive: "#18181b",
    itemTextColorActiveHover: "#18181b",
    itemIconColor: "#8a8a94",
    itemIconColorHover: "#18181b",
    itemIconColorActive: "#18181b",
    itemIconColorActiveHover: "#18181b",
    arrowColor: "#8a8a94",
    arrowColorHover: "#18181b",
  },
  Layout: {
    color: "#ffffff",
    siderColor: "#f7f7f8",
  },
  // 克制使用 border：卡片不再描边，内容直接落在白底上，靠留白与间距分层
  Card: {
    borderColor: "transparent",
    // 关闭按钮：加大悬停背景并缩小图标，避免背景紧贴图标（naive 默认 22px 背景 / 18px 图标）
    closeSize: "30px",
    closeIconSize: "16px",
  },
  // 抽屉 / 对话框的关闭按钮同样处理，保持三处一致
  Drawer: {
    closeSize: "30px",
    closeIconSize: "16px",
  },
  Dialog: {
    closeSize: "30px",
    closeIconSize: "16px",
  },
  // 用户下拉：加内边距、隐藏分割线、选项更宽松
  Dropdown: {
    padding: "8px",
    dividerColor: "transparent",
    optionHeightMedium: "38px",
    borderRadius: "8px",
    optionColorHover: "rgba(0, 0, 0, 0.045)",
    optionTextColor: "#3f3f46",
    optionTextColorHover: "#18181b",
  },
  // 提示消息：深蓝胶囊、白字、无图标
  Message: {
    borderRadius: "999px",
    border: "2px solid rgba(255, 255, 255, 0.16)",
    padding: "10px 24px",
    minWidth: "auto",
    maxWidth: "480px",
    fontSize: "13px",
    // 收起图标：尺寸与间距都归零（配合全局 CSS 一并隐藏）
    iconSize: "0",
    iconMargin: "0",
    color: "#062e6f",
    colorInfo: "#062e6f",
    colorSuccess: "#062e6f",
    colorError: "#062e6f",
    colorWarning: "#062e6f",
    colorLoading: "#062e6f",
    textColor: "#ffffff",
    textColorInfo: "#ffffff",
    textColorSuccess: "#ffffff",
    textColorError: "#ffffff",
    textColorWarning: "#ffffff",
    textColorLoading: "#ffffff",
    closeIconColor: "rgba(255, 255, 255, 0.75)",
    closeIconColorHover: "#ffffff",
    closeIconColorPressed: "#ffffff",
    closeIconColorInfo: "rgba(255, 255, 255, 0.75)",
    closeIconColorSuccess: "rgba(255, 255, 255, 0.75)",
    closeIconColorError: "rgba(255, 255, 255, 0.75)",
    closeIconColorWarning: "rgba(255, 255, 255, 0.75)",
    closeIconColorLoading: "rgba(255, 255, 255, 0.75)",
    closeColorHover: "rgba(255, 255, 255, 0.12)",
    closeColorPressed: "rgba(255, 255, 255, 0.2)",
    // 提示消息按类型读取 box-shadow 变量，需补齐各类型变体，
    // 否则会回退到默认的黑色阴影
    boxShadow: "0 8px 24px rgba(6, 46, 111, 0.24)",
    boxShadowInfo: "0 8px 24px rgba(6, 46, 111, 0.24)",
    boxShadowSuccess: "0 8px 24px rgba(6, 46, 111, 0.24)",
    boxShadowError: "0 8px 24px rgba(6, 46, 111, 0.24)",
    boxShadowWarning: "0 8px 24px rgba(6, 46, 111, 0.24)",
    boxShadowLoading: "0 8px 24px rgba(6, 46, 111, 0.24)",
  },
}));
</script>

<template>
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN" :theme-overrides="themeOverrides">
    <n-global-style />
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
