<script setup lang="ts">
import { darkTheme, NConfigProvider, NMessageProvider, NGlobalStyle } from "naive-ui";
</script>

<template>
  <n-config-provider :theme="darkTheme">
    <n-global-style />
    <n-message-provider>
      <router-view />
    </n-message-provider>
  </n-config-provider>
</template>

<style>
html,
body,
#app {
  margin: 0;
  min-height: 100%;
  background: #0b1020;
  color: #e8edf7;
  font-family: "Avenir Next", "PingFang SC", sans-serif;
}
</style>
